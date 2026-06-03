// Token generation CLI for the LiveKit HD spike.
//
// Usage:
//   cd experimental/livekit/token-gen
//   go run . --room voxlane-hd-spike --identity voxlane-publisher
//
// Reads LIVEKIT_API_KEY and LIVEKIT_API_SECRET from
// experimental/livekit/.env (loaded via godotenv).
//
// Outputs the JWT to stdout. No secrets are written to disk.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"github.com/livekit/protocol/auth"
)

func main() {
	room := flag.String("room", "voxlane-hd-spike", "room name")
	identity := flag.String("identity", "voxlane-publisher", "participant identity")
	ttl := flag.Duration("ttl", time.Hour, "token TTL")
	canPublish := flag.Bool("publish", true, "can publish tracks")
	canSubscribe := flag.Bool("subscribe", true, "can subscribe to tracks")
	flag.Parse()

	if err := loadEnv(); err != nil {
		log.Fatalf("env: %v", err)
	}

	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		log.Fatal("LIVEKIT_API_KEY and LIVEKIT_API_SECRET must be set (see experimental/livekit/.env.example)")
	}

	at := auth.NewAccessToken(apiKey, apiSecret)
	at.AddGrant(&auth.VideoGrant{
		RoomJoin:       true,
		Room:           *room,
		CanPublish:     canPublish,
		CanSubscribe:   canSubscribe,
		CanPublishData: &[]bool{true}[0],
	})
	at.SetIdentity(*identity)
	at.SetValidFor(*ttl)
	token, err := at.ToJWT()
	if err != nil {
		log.Fatalf("token: %v", err)
	}

	fmt.Print(token)
}

func loadEnv() error {
	candidates := []string{
		".env",
		filepath.Join("..", ".env"),
		filepath.Join("..", "..", ".env"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return godotenv.Load(p)
		}
	}
	return nil
}
