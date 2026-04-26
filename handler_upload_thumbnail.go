package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

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
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	fmt.Println(contentType)

	video, err := cfg.db.GetVideo(videoID)
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized access to file", err)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentType)

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file extension", err)
		return
	}

	var ext string

	if mediaType == "image/jpeg" {
		ext = ".jpeg"
	}
	if mediaType == "image/png" {
		ext = ".png"
	}

	assetFilePath := filepath.Join(cfg.assetsRoot, videoID.String()+ext)
	fmt.Println(assetFilePath)

	newFile, err := os.Create(assetFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not create file", err)
		return
	}
	io.Copy(newFile, file)

	updatedVideo := video
	url := fmt.Sprintf("http://localhost:%s/assets/%s%s", cfg.port, videoID, ext)
	fmt.Println(url)
	updatedVideo.ThumbnailURL = &url

	cfg.db.UpdateVideo(updatedVideo)
	respondWithJSON(w, http.StatusOK, updatedVideo)
}
