package main

import (
	"fmt"
	"time"

	htevents "github.com/ht-sdks/events-sdk-go"
)

func main() {
	client, _ := htevents.NewWithConfig("<write_key>", htevents.Config{
		Interval:  30 * time.Second,
		BatchSize: 100,
		Verbose:   true,
	})
	defer client.Close()

	client.Enqueue(htevents.Page{
		Name:   "Getting started",
		UserId: "123",
		Properties: htevents.Properties{
			"total": 29.99,
		},
	})

	done := time.After(1 * time.Second)
	tick := time.Tick(50 * time.Millisecond)

	for {
		select {
		case <-done:
			fmt.Println("exiting")
			return

		case <-tick:
			if err := client.Enqueue(htevents.Track{
				Event:  "Download",
				UserId: "123456",
				Properties: htevents.Properties{
					"application": "HT Desktop",
					"version":     "1.1.0",
					"platform":    "osx",
				},
			}); err != nil {
				fmt.Println("error:", err)
				return
			}
		}
	}
}
