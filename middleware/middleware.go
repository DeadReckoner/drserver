package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)




func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context){
		authHeader := c.GetHeader("Authorization")
		if authHeader == ""{
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Auth header missing"})
			c.Abort()
			return
		}


		accessToken := strings.TrimPrefix(authHeader, "Bearer ")


		getUser(accessToken)

	}

}

// curl -X GET 'http://127.0.0.1:54321/auth/v1/user' \
// -H "apikey: SUPABASE_KEY" \
// -H "Authorization: Bearer USER_TOKEN"


func getUser(accessToken string){
    godotenv.Load(".env")
	// supabaseURL := os.Getenv("API_URL")
	supabaseKey := os.Getenv("anon_key")


	req, err := http.NewRequest("GET", "http://127.0.0.1:54321/auth/v1/user", nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}
	req.Header.Add("apiKey", supabaseKey)
	req.Header.Add("Authorization", "Bearer "+ accessToken)

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return
	}

	fmt.Println("Response Status:", resp.Status)
	fmt.Println("Response Body:", string(body))

}