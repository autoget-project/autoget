package main

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/rs/zerolog/log"

	"github.com/autoget-project/autoget/backend/indexers/mteam/prefetcheddata"
)

// This is a tools used to update categories.
func main() {
	var outputFile string
	var apiKey string

	flag.StringVar(&outputFile, "o", "", "Output file path for the JSON")
	flag.StringVar(&apiKey, "k", "", "M-Team API Key")
	flag.Parse()

	if outputFile == "" {
		log.Fatal().Msg("Output file path (-o) is required")
	}
	if apiKey == "" {
		log.Fatal().Msg("API Key (-k) is required")
	}

	log.Info().Msg("Fetching from M-Team API...")
	data, err := prefetcheddata.FetchAll(apiKey, true)
	if err != nil {
		log.Fatal().Msgf("Failed to fetch: %v", err)
	}
	log.Info().Msg("Successfully fetched.")

	log.Info().Msgf("Encoding to JSON and writing to %s...", outputFile)
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatal().Msgf("Failed to marshal to JSON: %v", err)
	}

	err = os.WriteFile(outputFile, jsonData, 0644)
	if err != nil {
		log.Fatal().Msgf("Failed to write to file %s: %v", outputFile, err)
	}

	log.Info().Msgf("Successfully wrote to %s", outputFile)
}
