package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	videoIDStr := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid video ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "could not find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "could not validate JWT", err)
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

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to parse media type from content type header", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "media type not video/mp4", nil)
		return
	}
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "missing Content-Type for video", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to create temp video file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err = io.Copy(tempFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to save video to temp file", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to reset file pointer", err)
		return
	}

	processedFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to process video for fast start", err)
		return
	}
	defer os.Remove(processedFilePath)

	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to open processed video file", err)
		return
	}
	defer processedFile.Close()

	fileKeyPrefix, err := getVideoAspectRatio(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to get video aspect ratio", err)
	}
	fileKey := getAssetPath(mediaType)
	finalFileKey := fileKeyPrefix + "/" + fileKey

	s3PutObjectInput := &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(finalFileKey),
		Body:        processedFile,
		ContentType: aws.String(mediaType),
	}

	_, err = cfg.s3Client.PutObject(r.Context(), s3PutObjectInput)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to upload file to s3 bucket", err)
		return
	}

	video_url := fmt.Sprintf("%s,%s", cfg.s3Bucket, finalFileKey)
	video.VideoURL = &video_url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to update video", err)
		return
	}

	video, err = cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to generate presigned URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	s3PresignClient := s3.NewPresignClient(s3Client)
	presignedHTTPRequest, err := s3PresignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("error generating presigned HTTP request: %v", err)
	}
	return presignedHTTPRequest.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}

	splitURL := strings.Split(*video.VideoURL, ",")
	if len(splitURL) < 2 {
		return video, nil
	}

	bucket := splitURL[0]
	key := splitURL[1]
	presignedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Minute*5)
	if err != nil {
		return video, err
	}
	video.VideoURL = &presignedURL
	return video, nil
}
