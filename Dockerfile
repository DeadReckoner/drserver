# Specifies a parent image
FROM golang:1.23.5-bullseye
 
# Creates an app directory to hold your app’s source code
WORKDIR /app
 
# Copies everything from your root directory into /app
COPY main.go .
 
# Installs Go dependencies
RUN go mod download
 
# Builds your app with optional configuration
RUN go build -o /drserver
 
# Tells Docker which network port your container listens on
EXPOSE 8080
 
# Specifies the executable command that runs when the container starts
CMD [ “/drserver” ]