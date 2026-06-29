package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()
	apiKey := os.Getenv("GEMINI_API_KEY")

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	iter := client.ListModels(ctx)
	fmt.Println("Available Gemini models:")
	for {
		m, err := iter.Next()
		if err != nil {
			break
		}
		fmt.Printf("- %s (supports: %v)\n", m.Name, m.SupportedGenerationMethods)
	}
}
