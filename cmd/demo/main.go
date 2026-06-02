// Command demo provides an interactive command line utility to seed jobs,
// generate text embeddings, and find matching records in the Supabase database.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"orchid/pkg/embedding"
	"orchid/pkg/repository"
	"orchid/pkg/task"
	"orchid/pkg/utils"

	"github.com/jackc/pgx/v5"
)

func main() {
	utils.LoadEnv() // Load local .env file variables

	fmt.Println("==========================================================================")
	fmt.Println("                     Orchid Vector Embedding CLI Demo                     ")
	fmt.Println("==========================================================================")

	providerFlag := flag.String("provider", "openai", "AI Provider: 'openai' or 'gemini'")
	apiKeyFlag := flag.String("apikey", "", "API key (optional; defaults to env variables)")
	dbFlag := flag.String("db", "", "Supabase Postgres Connection URL")
	textFlag := flag.String("text", "Looking for a Go developer who knows PostgreSQL and Redis", "Text/Resume to match against jobs")
	seedFlag := flag.Bool("seed", false, "Seed DB with sample jobs before running search")
	limitFlag := flag.Int("limit", 3, "Maximum number of matched jobs to return")
	similarityFlag := flag.Float64("similarity", 0.75, "Minimum similarity score threshold (0.0 to 1.0)")
	flag.Parse()

	ctx := context.Background()

	// 1. Setup Embedding Client
	fmt.Printf("[1/4] Initializing %s embedding client...\n", *providerFlag)
	embConfig := embedding.Config{
		Provider: embedding.Provider(*providerFlag),
		APIKey:   *apiKeyFlag,
	}

	client, err := embedding.NewClient(embConfig)
	if err != nil {
		log.Fatalf("Error constructing client: %v\nMake sure the appropriate environment variable (OPENAI_API_KEY or GEMINI_API_KEY) is set or passed via -apikey", err)
	}
	fmt.Printf("      Initialized successfully. Dimension: %d\n", client.Dimension())

	// 2. Setup Database Connection if provided
	var repo repository.JobRepository
	var conn *pgx.Conn

	dbURL := *dbFlag
	if dbURL == "" {
		dbURL = os.Getenv("SUPABASE_DB_URL")
	}

	if dbURL != "" {
		escapedURL := utils.EscapeConnectionURI(dbURL)
		fmt.Println("[2/4] Connecting to Supabase database...")
		conn, err = pgx.Connect(ctx, escapedURL)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
		defer conn.Close(ctx)
		repo = repository.NewPostgresJobRepository(conn)
		fmt.Println("      Database connection established.")

		// Ensure schema is set up if we are seeding
		if *seedFlag {
			fmt.Println("      Applying jobs schema and seeding sample jobs...")
			_, err = conn.Exec(ctx, `
				CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
				CREATE EXTENSION IF NOT EXISTS vector;
				CREATE TABLE IF NOT EXISTS jobs (
					id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
					title VARCHAR(255) NOT NULL,
					company VARCHAR(255) NOT NULL,
					location VARCHAR(255),
					description TEXT NOT NULL,
					embedding VECTOR,
					created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
				);
			`)
			if err != nil {
				log.Fatalf("Failed to initialize table: %v", err)
			}

			// Clean up previous seeds
			_, _ = conn.Exec(ctx, "DELETE FROM jobs WHERE company = 'DemoCorp'")

			seedJobs(ctx, repo, client)
			fmt.Println("      Successfully seeded 4 jobs with precalculated vectors.")
		}
	} else {
		fmt.Println("[2/4] Database URL not provided. Running in Mock DB mode.")
		mockRepo := &MockJobRepoForCLI{}
		seedMockJobs(mockRepo, client.Dimension())
		repo = mockRepo
		fmt.Println("      Mock database populated with sample jobs.")
	}

	// 3. Instantiate and run Embedding Task
	fmt.Println("[3/4] Running Generate-Embedding workflow task...")
	embTask := task.NewEmbeddingTask(client)
	inputMap := task.EmbeddingTaskInput{Text: *textFlag}
	inputBytes, _ := json.Marshal(inputMap)

	outputBytes, err := embTask.Execute(ctx, inputBytes)
	if err != nil {
		log.Fatalf("Embedding task failed: %v", err)
	}

	var embOutput task.EmbeddingTaskOutput
	_ = json.Unmarshal(outputBytes, &embOutput)
	fmt.Printf("      Generated embedding vector successfully. Vector length: %d\n", len(embOutput.Embedding))

	// 4. Instantiate and run Job Matching Task
	fmt.Println("[4/4] Running Match-Jobs workflow task...")
	matchTask := task.NewMatchingTask(repo, client)
	matchInput := task.MatchingTaskInput{
		Embedding: embOutput.Embedding,
		Limit:     *limitFlag,
		MinScore:  *similarityFlag,
	}
	matchInputBytes, _ := json.Marshal(matchInput)

	matchOutputBytes, err := matchTask.Execute(ctx, matchInputBytes)
	if err != nil {
		log.Fatalf("Matching task failed: %v", err)
	}

	var matchOutput task.MatchingTaskOutput
	_ = json.Unmarshal(matchOutputBytes, &matchOutput)

	fmt.Println("\n==========================================================================")
	fmt.Printf("MATCH RESULTS (Similarity threshold: %.2f, Limit: %d)\n", *similarityFlag, *limitFlag)
	fmt.Println("==========================================================================")
	if len(matchOutput.Matches) == 0 {
		fmt.Println("No matching jobs found with score above threshold.")
	} else {
		for i, m := range matchOutput.Matches {
			fmt.Printf("%d. [%s] %s at %s (%s)\n", i+1, m.ID[:8], m.Title, m.Company, m.Location)
			fmt.Printf("   Similarity Score: %.4f\n", m.Similarity)
			fmt.Printf("   Description:      %s\n", m.Description)
			fmt.Println("--------------------------------------------------------------------------")
		}
	}
}

// Seed helper for actual DB
func seedJobs(ctx context.Context, repo repository.JobRepository, client embedding.Client) {
	jobs := []repository.Job{
		{
			Title:       "Backend Go Engineer",
			Company:     "DemoCorp",
			Location:    "Remote (US)",
			Description: "We are seeking a Go developer experienced with PostgreSQL, pgvector, and Redis to build workflow engines.",
		},
		{
			Title:       "React Frontend Specialist",
			Company:     "DemoCorp",
			Location:    "Remote (EU)",
			Description: "Build interactive dashboard interfaces using React, TypeScript, TailwindCSS, and state management tools.",
		},
		{
			Title:       "Senior Staff Software Engineer - Go/DB",
			Company:     "DemoCorp",
			Location:    "San Francisco, CA",
			Description: "High-scale Go system designer. Strong knowledge of database indices, transaction pools, and background worker queues in Golang.",
		},
		{
			Title:       "Technical Product Manager",
			Company:     "DemoCorp",
			Location:    "Austin, TX",
			Description: "Lead job-search automation product direction. Bridge engineering teams, client API requirements, and roadmap planning.",
		},
	}

	for i := range jobs {
		j := &jobs[i]
		// Populate embedding vector
		v, err := client.GetEmbedding(ctx, j.Description)
		if err != nil {
			log.Fatalf("Failed to generate embedding during seeding: %v", err)
		}
		j.Embedding = v
		if err := repo.InsertJob(ctx, j); err != nil {
			log.Fatalf("Failed to insert seed job: %v", err)
		}
	}
}

// Mock Repository for local offline demo execution
type MockJobRepoForCLI struct {
	jobs []repository.Job
}

func (m *MockJobRepoForCLI) InsertJob(ctx context.Context, job *repository.Job) error {
	m.jobs = append(m.jobs, *job)
	return nil
}

func (m *MockJobRepoForCLI) MatchJobs(ctx context.Context, vector []float32, limit int, minSimilarity float64) ([]repository.JobMatch, error) {
	var matches []repository.JobMatch
	for _, j := range m.jobs {
		// Calculate simple dot product similarity for mock matching
		similarity := dotProduct(vector, j.Embedding)
		// Restrict mock similarity value between 0.60 and 0.95 to simulate realistic values
		normSimilarity := float64(0.6 + 0.35*(similarity+1.0)/2.0)
		if normSimilarity > minSimilarity {
			matches = append(matches, repository.JobMatch{
				Job:        j,
				Similarity: normSimilarity,
			})
		}
	}
	// Sort by similarity descending
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[i].Similarity < matches[j].Similarity {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func seedMockJobs(repo *MockJobRepoForCLI, dim int) {
	descriptions := []string{
		"We are seeking a Go developer experienced with PostgreSQL, pgvector, and Redis to build workflow engines.",
		"Build interactive dashboard interfaces using React, TypeScript, TailwindCSS, and state management tools.",
		"High-scale Go system designer. Strong knowledge of database indices, transaction pools, and background worker queues in Golang.",
		"Lead job-search automation product direction. Bridge engineering teams, client API requirements, and roadmap planning.",
	}
	titles := []string{"Backend Go Engineer", "React Frontend Specialist", "Senior Staff Software Engineer - Go/DB", "Technical Product Manager"}
	locations := []string{"Remote (US)", "Remote (EU)", "San Francisco, CA", "Austin, TX"}

	for i := 0; i < 4; i++ {
		v := make([]float32, dim)
		// Generate deterministic mock vector
		for d := 0; d < dim; d++ {
			v[d] = float32((i*7 + d*13) % 100) / 100.0
		}
		repo.jobs = append(repo.jobs, repository.Job{
			ID:          fmt.Sprintf("%d0000000-0000-0000-0000-000000000000", i+1),
			Title:       titles[i],
			Company:     "DemoCorp",
			Location:    "MockCity",
			Description: descriptions[i] + " (" + locations[i] + ")",
			Embedding:   v,
		})
	}
}

func dotProduct(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
