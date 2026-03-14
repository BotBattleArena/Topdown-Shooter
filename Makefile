.PHONY: build clean

build:
	go mod tidy
	go build -o bin/topdown-shooter.exe ./cmd/topdownshooter

clean:
	@cmd /c if exist bin rmdir /s /q bin