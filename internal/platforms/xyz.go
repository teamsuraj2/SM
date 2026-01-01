package platforms

import (
        "context"
        "errors"
        "fmt"
        "io"
        "net/http"
        "os"

        "github.com/amarnathcjd/gogram/telegram"
                        "github.com/TheTeamVivek/YukkiMusic/internal/state"
)

var (
        apiBase = "https://youtubify.me"
        apiKey  = os.Getenv("YT_API_KEY")
)
const PlatformYoutubify state.PlatformName = "xyz"
type YoutubifyPlatform struct{}

func init() {
        addPlatform(100, PlatformYoutubify, &YoutubifyPlatform{})
}

func (*YoutubifyPlatform) Name() state.PlatformName {
        return PlatformYoutubify
}

func (*YoutubifyPlatform) IsValid(query string) bool {
        return false
}

func (*YoutubifyPlatform) GetTracks(query string) ([]*state.Track, error) {
        return nil, errors.New("Youtubify is a direct download platform")
}

func (*YoutubifyPlatform) IsDownloadSupported(source state.PlatformName) bool {
        return source == state.PlatformYouTube
}

func (f *YoutubifyPlatform) Download(_ context.Context, track *state.Track, _ *telegram.NewMessage) (string, error) {
        return downloadAudio(track.ID)
}

func downloadAudio(videoID string) (string, error) {
        filepath := fmt.Sprintf("downloads/%s.mp3", videoID)

        if _, err := os.Stat(filepath); err == nil {
                return filepath, nil
        }

        if err := os.MkdirAll("downloads", 0755); err != nil {
                return "", err
        }

        client := &http.Client{}
        url := fmt.Sprintf("%s/download/audio?video_id=%s&mode=download&no_redirect=1&api_key=%s", apiBase, videoID, apiKey)

        resp, err := client.Get(url)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()

        if resp.StatusCode != 200 {
                return "", fmt.Errorf("API returned %s", resp.Status)
        }

        out, err := os.Create(filepath)
        if err != nil {
                return "", err
        }
        defer out.Close()

        _, err = io.Copy(out, resp.Body)
        if err != nil {
                return "", err
        }

        return filepath, nil
}
