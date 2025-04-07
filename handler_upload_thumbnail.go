package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to parse form file", err)
		return
	}

	defer file.Close()

	var fileExtension string
	fileType := header.Header.Get("Content-Type")
	if fileType == "" {
		respondWithError(w, http.StatusBadRequest, "missing Content-Type for thumbnail", nil)
		return
	}
	fileTypeParts := strings.Split(fileType, "/")
	if len(fileTypeParts) == 2 {
		fileExtension = "." + fileTypeParts[1]
	}

	fileName := videoIDString + fileExtension
	filePath := filepath.Join(cfg.assetsRoot, fileName)
	fmt.Println(filePath)
	newFile, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to create file", err)
		return
	}
	defer newFile.Close()

	_, err = io.Copy(newFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to copy image data to new file", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		if userID != video.UserID {
			respondWithError(w, http.StatusUnauthorized, "user is not the video owner", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "unable to retrieve video metadata", err)
		return
	}

	thumbnail_url := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, fileName)

	video.ThumbnailURL = &thumbnail_url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
