package main

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-playground/validator/v10"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

type SignupUser struct {
	Email           string `json:"email" validate:"required,email,min=5,max=20"`
	Password        string `json:"password" validate:"required,min=8"`
	ConfirmPassword string `json:"confirm_password" validate:"required,eqfield=Password"`
	Role            string `json:"role" validate:"required,oneof=admin guest"`
}

type LoginUser struct {
	Email    string `json:"email" validate:"required,email,min=5,max=20"`
	Password string `json:"password" validate:"required,min=8"`
}

type User struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type Claims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.StandardClaims
}

var db *sql.DB

func hashPassword(password string, cost int) (string, error) {
	sha512Hash := sha512.New()
	sha512Hash.Write([]byte(password))
	hashed := sha512Hash.Sum(nil)

	bytes, err := bcrypt.GenerateFromPassword([]byte(hashed), cost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func generateRandomKey(length int) ([]byte, error) {
	key := make([]byte, length)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	return key, nil
}

var jwtKey = []byte("")

var validate *validator.Validate

func main() {
	var err error

	validate = validator.New()

	db, err = sql.Open("mysql", "root:@tcp(localhost:3306)/Sekertaris")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	r := mux.NewRouter()

	r.HandleFunc("/daftar", SignupHandler).Methods("POST")
	r.HandleFunc("/login", SigninHandler).Methods("POST")
	// r.HandleFunc("/dashboard", DashboardHandler).Methods("GET")

	corsMiddleware := handlers.CORS(
		handlers.AllowedOrigins([]string{"https://testing-riset.vercel.app/"}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
		handlers.AllowCredentials(),
	)

	fmt.Println("Server is running on port 4000")
	log.Fatal(http.ListenAndServe(":4000", corsMiddleware(r)))
}

func SignupHandler(w http.ResponseWriter, r *http.Request) {
	var user SignupUser

	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		log.Println("Error decoding JSON:", err)
		http.Error(w, `{"Error Message": "Invalid JSON format"}`, http.StatusBadRequest)
		return
	}

	err = validate.Struct(user)
	if err != nil {
		log.Println("Validation error:", err)
		http.Error(w, `{"Error Message": "Invalid input data"}`, http.StatusBadRequest)
		return
	}

	var existingUser string
	err = db.QueryRow("SELECT email FROM user WHERE email = ?", user.Email).Scan(&existingUser)
	if err == nil {
		http.Error(w, `{"Error Message": "Email sudah ada silahkan masukan email lain"}`, http.StatusBadRequest)
		return
	} else if err != sql.ErrNoRows {
		log.Println("Error checking email:", err)
		http.Error(w, `{"Error Message": "Error processing request"}`, http.StatusInternalServerError)
		return
	}

	if user.Password != user.ConfirmPassword {
		http.Error(w, `{"Error Message": "Password tidak sama"}`, http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), 14)
	if err != nil {
		log.Println("Error hashing password:", err)
		http.Error(w, `{"Error Message": "Error processing request"}`, http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("INSERT INTO user (email, password, role) VALUES (?, ?, ?)", user.Email, hashedPassword, user.Role)
	if err != nil {
		http.Error(w, `{"Error Message": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message": "User created successfully"}`))
}
func SigninHandler(w http.ResponseWriter, r *http.Request) {
	var user LoginUser
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		log.Println("Error decoding JSON:", err)
		http.Error(w, `{"Error Message": "Invalid JSON format"}`, http.StatusBadRequest)
		return
	}

	err = validate.Struct(user)
	if err != nil {
		log.Println("Validation error:", err)
		http.Error(w, `{"Error Message": "Invalid input data"}`, http.StatusBadRequest)
		return
	}

	var storedPassword, role string
	err = db.QueryRow("SELECT password, role FROM user WHERE email = ?", user.Email).Scan(&storedPassword, &role)
	if err != nil {
		log.Println("Error retrieving password from database:", err)
		http.Error(w, `{"Error Message": "Invalid Email or password"}`, http.StatusUnauthorized)
		return
	}
	if !checkPasswordHash(user.Password, storedPassword) {
		http.Error(w, `{"Error Message": "Invalid Email or password"}`, http.StatusUnauthorized)
		return
	}

	expirationTime := time.Now().Add(5 * time.Minute)
	claims := &Claims{
		Email: user.Email,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	jwtKey, err = generateRandomKey(32)
	if err != nil {
		log.Println("Error generating JWT key:", err)
		http.Error(w, `{"Error Message": "Error processing request"}`, http.StatusInternalServerError)
		return
	}

	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		log.Println("Error signing JWT token:", err)
		http.Error(w, `{"Error Message": "Error processing request"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Authorization", "Bearer "+tokenString)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"token": "` + tokenString + `", "redirect": "dashboard"}`))
}

func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"Error Message": "Missing authorization header"}`, http.StatusUnauthorized)
			return
		}

		tokenString := authHeader[len("Bearer "):]
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtKey, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, `{"Error Message": "Invalid token"}`, http.StatusUnauthorized)
			return
		}

		context1 := context.WithValue(r.Context(), "Email", claims.Email)
		context1 = context.WithValue(context1, "Role", claims.Role)
		next.ServeHTTP(w, r.WithContext(context1))
	})
}
