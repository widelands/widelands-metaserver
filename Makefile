all:
	go build -o bin/ ./wlms ./wlnr

cross:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/linux_amd64/ ./wlms ./wlnr
