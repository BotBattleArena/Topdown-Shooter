.PHONY: build clean

ifeq ($(findstring sh,$(SHELL)),sh)
    MKDIR = mkdir -p $(1)
    RMDIR = rm -rf $(1)
else
    MKDIR = if not exist "$(subst /,\,$(1))" mkdir "$(subst /,\,$(1))"
    RMDIR = if exist "$(subst /,\,$(1))" rmdir /s /q "$(subst /,\,$(1))"
endif

build:
	@$(call MKDIR,bin)
	go mod tidy
	go build -o bin/topdown-shooter.exe ./cmd/topdownshooter

clean:
	@$(call RMDIR,bin)