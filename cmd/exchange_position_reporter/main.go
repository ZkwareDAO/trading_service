package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"trading-service/internal/config"
	"trading-service/internal/reporter"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("[Config] API URL: %s", cfg.Reporter.APIURL)
	runReport(cfg)
}

func runReport(cfg *config.Config) {
	log.Println("Starting position report...")

	usersFile := fmt.Sprintf("%s/users.csv", cfg.Storage.DataDir)
	users, err := reporter.ReadUsers(usersFile)
	if err != nil {
		log.Printf("Failed to read users: %v", err)
		os.Exit(1)
	}

	log.Printf("Found %d users", len(users))

	for _, user := range users {
		positions, err := reporter.FetchExchangePositions(cfg.Reporter.APIURL, user.Name, user.Exchange)
		if err != nil {
			log.Printf("Failed to fetch positions for %s/%s: %v", user.Name, user.Exchange, err)
			continue
		}

		if len(positions) == 0 {
			log.Printf("No positions for %s/%s", user.Name, user.Exchange)
			continue
		}

		log.Printf("Found %d positions for %s/%s", len(positions), user.Name, user.Exchange)

		dateDir := fmt.Sprintf("%s/exchange_positions/%s",
			cfg.Storage.DataDir, time.Now().Format("20060102"))
		if err := reporter.SavePositionsToCSV(dateDir, positions); err != nil {
			log.Printf("Failed to save positions for %s/%s: %v", user.Name, user.Exchange, err)
			continue
		}

		log.Printf("Saved positions for %s/%s to %s", user.Name, user.Exchange, dateDir)

		if cfg.Notification.Enabled && cfg.Notification.TestURL != "" {
			message := reporter.FormatWeChatMessage(positions, user.Name, user.Exchange)
			if err := reporter.SendWeChatNotification(cfg.Notification.TestURL, message); err != nil {
				log.Printf("Failed to send WeChat notification for %s/%s: %v", user.Name, user.Exchange, err)
				continue
			}
			log.Printf("Sent WeChat notification for %s/%s", user.Name, user.Exchange)
		}
	}

	log.Println("Position report completed")
}
