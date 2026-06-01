-- ==========================================
-- Orchid Workflow Engine Schema Setup
-- Database: Supabase PostgreSQL with pgvector
-- ==========================================

-- Enable extensions required for our workflows
-- pgvector is pre-installed in Supabase, but must be enabled per database
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

-- Create jobs table
-- We use a dimension-less VECTOR column to support both 1536-dimension embeddings (OpenAI)
-- and 768-dimension embeddings (Gemini) dynamically within the same table schema.
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    company VARCHAR(255) NOT NULL,
    location VARCHAR(255),
    description TEXT NOT NULL,
    embedding VECTOR, -- Dimension-less vector (supported in pgvector 0.5.0+)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ==========================================
-- INDEXING RECOMMENDATIONS FOR PERFORMANCE
-- ==========================================
-- For large datasets, exact nearest neighbor searches can be slow. 
-- You should create an HNSW index once your vector dimensions are decided.
--
-- For OpenAI (1536 dimensions):
-- CREATE INDEX ON jobs USING hnsw (embedding vector_cosine_ops);
--
-- For Gemini (768 dimensions):
-- CREATE INDEX ON jobs USING hnsw (embedding vector_cosine_ops);
--
-- Note: A dimension-less column allows different row dimensions, but indexing 
-- requires all indexed values to have the same dimension. Ensure consistency
-- in the dimension size inserted if using HNSW/IVFFlat indexes.
-- ==========================================
