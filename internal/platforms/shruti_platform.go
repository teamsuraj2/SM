/*
 * This file is part of YukkiMusic.
 *
 * YukkiMusic — A Telegram bot that streams music into group voice chats with seamless playback and control.
 * Copyright (C) 2025 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

package platforms

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"
	"resty.dev/v3"

	state "main/internal/core/models"
)

const PlatformShrutiAPI state.PlatformName = "ShrutiAPI"

type ShrutiAPIPlatform struct {
	name   state.PlatformName
	apiURL string
}

type shrutiDownloadResponse struct {
	DownloadToken string `json:"download_token"`
}

func init() {
	// Priority 75 - between Fallen API (80) and DirectStream (65)
	Register(75, &ShrutiAPIPlatform{
		name:   PlatformShrutiAPI,
		apiURL: "https://shrutibots.site",
	})
}

func (s *ShrutiAPIPlatform) Name() state.PlatformName {
	return s.name
}

// IsValid - ShrutiAPI is download-only, doesn't validate URLs
func (s *ShrutiAPIPlatform) IsValid(query string) bool {
	return false
}

// GetTracks - ShrutiAPI is download-only platform
func (s *ShrutiAPIPlatform) GetTracks(
	_ string,
	_ bool,
) ([]*state.Track, error) {
	return nil, errors.New("shrutiapi is a download-only platform")
}

// IsDownloadSupported - supports YouTube downloads
func (s *ShrutiAPIPlatform) IsDownloadSupported(
	source state.PlatformName,
) bool {
	return source == PlatformYouTube
}

// Download - downloads audio/video from YouTube using Shruti API
func (s *ShrutiAPIPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {
	// Check cache first
	if path, err := checkDownloadedFile(track.ID); err == nil {
		gologging.InfoF("ShrutiAPI: Using cached file for %s", track.ID)
		return path, nil
	}

	gologging.InfoF("ShrutiAPI: Downloading %s", track.Title)

	// Ensure downloads directory exists
	if err := ensureDownloadsDir(); err != nil {
		gologging.ErrorF(
			"ShrutiAPI: Failed to create downloads directory: %v",
			err,
		)
		return "", fmt.Errorf("failed to create downloads directory: %w", err)
	}

	// Use track.ID as video ID (already extracted by YouTube platform)
	videoID := track.ID

	// Determine file extension and type
	mediaType := "audio"
	ext := ".mp3"
	if track.Video {
		mediaType = "video"
		ext = ".mp4"
	}

	filePath := filepath.Join("downloads", track.ID+ext)

	// Get download token
	token, err := s.getDownloadToken(ctx, videoID, mediaType)
	if err != nil {
		gologging.ErrorF("ShrutiAPI: Failed to get download token: %v", err)
		return "", fmt.Errorf("failed to get download token: %w", err)
	}

	// Download the file
	if err := s.downloadFile(ctx, videoID, mediaType, token, filePath); err != nil {
		os.Remove(filePath) // Clean up on error
		gologging.ErrorF("ShrutiAPI: Download failed: %v", err)
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Verify file exists and has content
	if stat, err := os.Stat(filePath); err != nil || stat.Size() == 0 {
		os.Remove(filePath)
		return "", errors.New("downloaded file is empty or missing")
	}

	gologging.InfoF("ShrutiAPI: Successfully downloaded %s", track.Title)
	return filePath, nil
}

// getDownloadToken requests a download token from the API
func (s *ShrutiAPIPlatform) getDownloadToken(
	ctx context.Context,
	videoID string,
	mediaType string,
) (string, error) {
	client := resty.New().
		SetTimeout(7 * time.Second)

	defer client.Close()

	var result shrutiDownloadResponse

	resp, err := client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"url":  videoID,
			"type": mediaType,
		}).
		SetResult(&result).
		Get(s.apiURL + "/download")

	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", fmt.Errorf("api request failed: %w", err)
	}

	if resp.IsError() {
		return "", fmt.Errorf("api returned status: %d", resp.StatusCode())
	}

	if result.DownloadToken == "" {
		return "", errors.New("empty download token received")
	}

	return result.DownloadToken, nil
}

// downloadFile downloads the actual media file
func (s *ShrutiAPIPlatform) downloadFile(
	ctx context.Context,
	videoID string,
	mediaType string,
	token string,
	filePath string,
) error {
	streamURL := fmt.Sprintf(
		"%s/stream/%s?type=%s&token=%s",
		s.apiURL,
		videoID,
		mediaType,
		token,
	)

	// Set timeout based on media type
	timeout := 300 * time.Second // 5 minutes for audio
	if mediaType == "video" {
		timeout = 600 * time.Second // 10 minutes for video
	}

	// Create HTTP client with context
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Create output file
	outFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	// Download with chunked reading
	buf := make([]byte, 16384) // 16KB chunks
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := outFile.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write error: %w", writeErr)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
	}

	return nil
}
