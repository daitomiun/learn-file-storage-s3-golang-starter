package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	video, err := cfg.db.GetVideo(videoID)
	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized access to file", err)
		return
	}

	const maxUploadLimit = 10 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadLimit)
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file extension", err)
		return
	}

	tmpFile, err := os.CreateTemp("", "tubely-upload.mp4")

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not process the request", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	io.Copy(tmpFile, file)

	tmpFile.Seek(0, io.SeekStart)

	outputFilePath, err := processVideoForFastStart(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not process the request", err)
		return
	}

	fastStartFile, err := os.Open(outputFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not process the request", err)
		return
	}
	defer os.Remove(fastStartFile.Name())
	defer fastStartFile.Close()

	aspectRatio, err := getVideoAspectRatio(fastStartFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not process the request", err)
		return
	}

	assetFilePath := fmt.Sprintf("%s/%s", aspectRatio, getAssetPath(mediaType))

	_, err = cfg.s3Client.PutObject(
		context.Background(),
		&s3.PutObjectInput{
			Bucket:      &cfg.s3Bucket,
			Key:         &assetFilePath,
			Body:        fastStartFile,
			ContentType: &contentType,
		},
	)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reach out to bucket", err)
		return
	}
	updatedVideo := video

	url := fmt.Sprintf("%s,%s", cfg.s3Bucket, assetFilePath)
	fmt.Printf("current url -> %s \n", url)

	updatedVideo.VideoURL = &url
	cfg.db.UpdateVideo(updatedVideo)

	signedVideo, err := cfg.dbVideoToSignedVideo(updatedVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not process the request", err)
		return
	}
	respondWithJSON(w, http.StatusOK, signedVideo)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	newClient := s3.NewPresignClient(s3Client)
	req, err := newClient.PresignGetObject(
		context.Background(),
		&s3.GetObjectInput{Bucket: &bucket, Key: &key},
		s3.WithPresignExpires(expireTime),
	)
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		// NOTE: if the value is old or hasn't updated, leave it as is and return
		return video, nil
	}
	params := strings.Split(*video.VideoURL, ",")
	if len(params) < 2 {
		return database.Video{}, errors.New("Invalid Params")
	}
	bucket, key := params[0], params[1]

	duration, err := time.ParseDuration("1h")
	if err != nil {
		return database.Video{}, err
	}

	url, err := generatePresignedURL(cfg.s3Client, bucket, key, duration)

	if err != nil {
		return database.Video{}, err
	}
	updatedVideo := video
	updatedVideo.VideoURL = &url

	return updatedVideo, nil
}
