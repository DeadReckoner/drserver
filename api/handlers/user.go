package handlers

import (
	"context"
	"dr/server/analytics"
	"dr/server/api/utils"
	"dr/server/auth/apple"
	"dr/server/database"
	"dr/server/email"
	"dr/server/log"
	"dr/server/storage"
	"dr/server/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	avatarsBucket = "avatars"
)

type UserHandler struct {
	db      *gorm.DB
	storage *storage.StorageClient
	apple   *apple.Client
	mailer  *email.Dispatcher
}

func NewUserHandler(db *gorm.DB, storageClient *storage.StorageClient, appleClient *apple.Client, mailer *email.Dispatcher) *UserHandler {
	return &UserHandler{db: db, storage: storageClient, apple: appleClient, mailer: mailer}
}

// sendWelcomeAsync fires the welcome email as a goroutine so the create
// response is not blocked on Resend. Dedupe key is the user_id so we never
// send a second welcome no matter how many times CreateUser is called for
// the same user (which happens routinely - the client calls it on every
// app launch in some flows).
func (h *UserHandler) sendWelcomeAsync(userID string) {
	if h.mailer == nil || userID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.mailer.Send(ctx, userID, email.KindWelcome, email.Vars{}, "welcome:"+userID); err != nil {
			log.Warn(ctx, "welcome email failed", err.Error())
		}
	}()
}

func (h *UserHandler) CreateUser(c *gin.Context) {
	ctx := c.Request.Context()
	var newUser types.User

	if !utils.BindJSONOrAbort(c, &newUser) {
		return
	}

	// Parse full_name into first_name/last_name if provided (for Apple/Google sign-in)
	newUser.ParseFullName()

	tx := database.CreateUser(c, newUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	h.sendWelcomeAsync(user.UserID)
	emitSignupIfNew(ctx, tx)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User upserted successfully")
}

// emitSignupIfNew emits signup_completed only for a genuinely new account.
//
// CreateUser is an upsert and the client calls it on every app launch in some
// flows, so emitting unconditionally would report a signup per launch and make
// the top of the funnel meaningless. database.CreateUser returns a zero-value
// *gorm.DB (RowsAffected 0) when the user already existed and the result of
// the INSERT (RowsAffected 1) when it created one, which is the only signal
// distinguishing the two.
func emitSignupIfNew(ctx context.Context, tx *gorm.DB) {
	if tx == nil || tx.RowsAffected != 1 {
		return
	}
	analytics.Emit(ctx, analytics.EventSignupCompleted, nil)
}

// CreateUserWithImage creates a user with an optional profile image.
// Accepts multipart/form-data with:
//   - "user": JSON string of user data
//   - "image": optional image file
func (h *UserHandler) CreateUserWithImage(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse user JSON from form field
	userJSON := c.PostForm("user")
	if userJSON == "" {
		utils.BadRequestResponse(c, "user field is required")
		return
	}

	var newUser types.User
	if err := json.Unmarshal([]byte(userJSON), &newUser); err != nil {
		utils.BadRequestResponse(c, "invalid user JSON: "+err.Error())
		return
	}

	newUser.ParseFullName()

	// Handle optional image file
	file, header, err := c.Request.FormFile("image")
	if err == nil {
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		if err != nil {
			utils.InternalErrorResponse(c, err)
			return
		}

		// Store under the authenticated user_id (the same id database.CreateUser
		// keys the row to), never the body-supplied UserID — storage bypasses
		// RLS, so a body value could target another user's folder.
		authID := c.GetString("user_id")
		if authID == "" {
			utils.ErrorResponse(c, http.StatusUnauthorized, "missing authenticated user")
			return
		}
		path := fmt.Sprintf("%s/profile", authID)
		contentType := header.Header.Get("Content-Type")

		if err := h.storage.Upload(ctx, avatarsBucket, path, contentType, data); err != nil {
			log.Error(ctx, "Failed to upload profile image", err)
			utils.InternalErrorResponse(c, err)
			return
		}

		newUser.ProfileImageURL = h.storage.PublicURL(avatarsBucket, path)
	}

	tx := database.CreateUser(c, newUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	h.sendWelcomeAsync(user.UserID)
	emitSignupIfNew(ctx, tx)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User created with image successfully")
}

func (h *UserHandler) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	// A user may only update their own record.
	if authID := c.GetString("user_id"); authID == "" || authID != userID {
		utils.ErrorResponse(c, http.StatusForbidden, "cannot update another user's profile")
		return
	}

	var updatedUser types.User
	if !utils.BindJSONOrAbort(c, &updatedUser) {
		return
	}

	// Parse full_name into first_name/last_name if provided
	updatedUser.ParseFullName()

	tx, prior := database.UpdateUserReturningPrior(userID, updatedUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	emitOnboardingIfNewlyComplete(ctx, prior, updatedUser)

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User updated successfully")
}

// emitOnboardingIfNewlyComplete fires onboarding_completed only on the
// false→true transition.
//
// Setup Activation is defined as onboarding completing within 24 hours of
// signup, so the event has to represent a moment, not a state. The client
// PATCHes the user record for many reasons and may include the flag every
// time; emitting on the flag alone would re-fire on every profile edit and
// push the activation rate past 100%.
func emitOnboardingIfNewlyComplete(ctx context.Context, prior, update types.User) {
	if !update.OnboardingCompleted || prior.OnboardingCompleted {
		return
	}
	analytics.Emit(ctx, analytics.EventOnboardingCompleted, nil)
}

// UpdateUserWithImage updates a user with an optional profile image.
// Accepts multipart/form-data with:
//   - "user": JSON string of user data
//   - "image": optional image file
func (h *UserHandler) UpdateUserWithImage(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	// The path :id must match the authenticated principal. Storage uses the
	// service-role key (RLS bypassed), so this check is the only thing stopping
	// a user from overwriting another user's avatar at avatars/<id>/profile.
	if authID := c.GetString("user_id"); authID == "" || authID != userID {
		utils.ErrorResponse(c, http.StatusForbidden, "cannot update another user's profile")
		return
	}

	// Parse user JSON from form field
	userJSON := c.PostForm("user")
	if userJSON == "" {
		utils.BadRequestResponse(c, "user field is required")
		return
	}

	var updatedUser types.User
	if err := json.Unmarshal([]byte(userJSON), &updatedUser); err != nil {
		utils.BadRequestResponse(c, "invalid user JSON: "+err.Error())
		return
	}

	updatedUser.ParseFullName()

	// Handle optional image file
	file, header, err := c.Request.FormFile("image")
	if err == nil {
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)
		if err != nil {
			utils.InternalErrorResponse(c, err)
			return
		}

		path := fmt.Sprintf("%s/profile", userID)
		contentType := header.Header.Get("Content-Type")

		if err := h.storage.Upload(ctx, avatarsBucket, path, contentType, data); err != nil {
			log.Error(ctx, "Failed to upload profile image", err)
			utils.InternalErrorResponse(c, err)
			return
		}

		updatedUser.ProfileImageURL = h.storage.PublicURL(avatarsBucket, path)
	}

	tx, prior := database.UpdateUserReturningPrior(userID, updatedUser)
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	emitOnboardingIfNewlyComplete(ctx, prior, updatedUser)

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User updated with image successfully")
}

func (h *UserHandler) GetUser(c *gin.Context) {
	ctx := c.Request.Context()
	user := database.GetUser(c)

	log.Info(ctx, "User: ", user)
	if user.UserID == "" {
		utils.NotFoundResponse(c, "User")
		return
	}

	utils.DataResponse(c, http.StatusOK, user)
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	// The path :id must match the authenticated principal — a user can only
	// delete their own account.
	if authID := c.GetString("user_id"); authID == "" || authID != userID {
		utils.ErrorResponse(c, http.StatusForbidden, "cannot delete another user's account")
		return
	}

	log.Info(ctx, "Deleting user", userID)

	// Apple requires deleted accounts to have their refresh token revoked. The
	// mobile client passes the original authorization code when the user
	// signed in with Apple. We log and continue on failure: a partial Apple
	// revocation should not block the user from deleting their account.
	if appleAuthCode := c.Query("apple_auth_code"); appleAuthCode != "" {
		if h.apple == nil {
			log.Warn(ctx, "apple_auth_code provided but Apple client is not configured", nil)
		} else if err := h.apple.RevokeByAuthCode(ctx, appleAuthCode); err != nil {
			log.Error(ctx, "Failed to revoke Apple Sign-In token", err)
		}
	}

	// Delete profile image from storage (ignore errors - image may not exist)
	path := fmt.Sprintf("%s/profile", userID)
	if err := h.storage.Delete(ctx, avatarsBucket, path); err != nil {
		log.Warn(ctx, "Failed to delete profile image (may not exist)", err)
	}

	if err := database.DeleteUser(userID); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	analytics.Emit(ctx, analytics.EventAccountDeleted, nil)

	utils.MessageResponse(c, http.StatusOK, "User deleted successfully")
	log.Info(ctx, "User deleted successfully")
}

// DeleteUserImage removes a user's profile image without deleting the user.
func (h *UserHandler) DeleteUserImage(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := utils.GetParamOrAbort(c, "id")
	if !ok {
		return
	}

	// Service-role storage bypasses RLS, so enforce ownership here: a user may
	// only delete their own avatar.
	if authID := c.GetString("user_id"); authID == "" || authID != userID {
		utils.ErrorResponse(c, http.StatusForbidden, "cannot delete another user's profile image")
		return
	}

	// Delete from storage
	path := fmt.Sprintf("%s/profile", userID)
	if err := h.storage.Delete(ctx, avatarsBucket, path); err != nil {
		log.Error(ctx, "Failed to delete profile image", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	// Clear the URL in the database
	tx := database.UpdateUser(userID, types.User{ProfileImageURL: ""})
	if tx.Error != nil {
		utils.InternalErrorResponse(c, tx.Error)
		return
	}

	user := database.GetUser(c)
	utils.DataResponse(c, http.StatusOK, user)
	log.Info(ctx, "User profile image deleted successfully")
}

func (h *UserHandler) SearchUsers(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		utils.BadRequestResponse(c, "email query parameter is required")
		return
	}

	users := database.SearchUsersByEmail(email)
	utils.DataResponse(c, http.StatusOK, users)
}
