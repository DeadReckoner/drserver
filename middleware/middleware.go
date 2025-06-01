package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

type AuthenticatedUser struct {
	ID               string    `json:"id"`
	Aud              string    `json:"aud"`
	Role             string    `json:"role"`
	Email            string    `json:"email"`
	EmailConfirmedAt time.Time `json:"email_confirmed_at"`
	Phone            string    `json:"phone"`
	ConfirmedAt      time.Time `json:"confirmed_at"`
	LastSignInAt     time.Time `json:"last_sign_in_at"`
	AppMetadata      struct {
		Provider  string   `json:"provider"`
		Providers []string `json:"providers"`
	} `json:"app_metadata"`
	UserMetadata struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		PhoneVerified bool   `json:"phone_verified"`
		Sub           string `json:"sub"`
	} `json:"user_metadata"`
	Identities []struct {
		IdentityID   string `json:"identity_id"`
		ID           string `json:"id"`
		UserID       string `json:"user_id"`
		IdentityData struct {
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			PhoneVerified bool   `json:"phone_verified"`
			Sub           string `json:"sub"`
		} `json:"identity_data"`
		Provider     string    `json:"provider"`
		LastSignInAt time.Time `json:"last_sign_in_at"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
	} `json:"identities"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	IsAnonymous bool      `json:"is_anonymous"`
}


func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context){
		authHeader := c.GetHeader("Authorization")
		if authHeader == ""{
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Auth header missing"})
			c.Abort()
			return
		}

		accessToken := strings.TrimPrefix(authHeader, "Bearer ")
		user_id := parseToken(accessToken)

		if user_id == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token or user ID not found"})
			c.Abort()
			return
		}

		c.Set("user_id", user_id)
		c.Next()
	}

}

// curl -X GET 'http://127.0.0.1:54321/auth/v1/user' \
// -H "apikey: SUPABASE_KEY" \
// -H "Authorization: Bearer USER_TOKEN"


func parseToken(accessToken string) string {
    godotenv.Load(".env")
	// supabaseURL := os.Getenv("API_URL")
	supabaseKey := os.Getenv("anon_key")
	supabaseApi := os.Getenv("API_URL")


	req, err := http.NewRequest("GET", supabaseApi + "/auth/v1/user", nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return ""
	}
	req.Header.Add("apiKey", supabaseKey)
	req.Header.Add("Authorization", "Bearer "+ accessToken)

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return ""
	}

	fmt.Println("Response Status:", resp.Status)
	fmt.Println("Response Body:", string(body))

	var user AuthenticatedUser
	if err := json.Unmarshal(body, &user); err != nil {
		fmt.Println("unable to unmarshall json")
	}
	return user.ID

}