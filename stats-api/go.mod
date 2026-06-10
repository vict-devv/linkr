module github.com/linkr/stats-api

go 1.22.0

require (
	github.com/linkr/shared v0.0.0-00010101000000-000000000000
	go.mongodb.org/mongo-driver/v2 v2.0.0
	golang.org/x/sync v0.9.0
)

replace github.com/linkr/shared => ../shared

require (
	github.com/golang/snappy v0.0.4 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/klauspost/compress v1.16.7 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.29.0 // indirect
	golang.org/x/text v0.20.0 // indirect
)
