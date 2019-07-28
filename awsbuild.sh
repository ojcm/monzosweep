# Creates zip file suitable for upload to AWS Lambda
GOARCH=amd64 GOOS=linux go build main.go
zip main.zip main
rm main
