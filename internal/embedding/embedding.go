package embedding

import "context"

type Model struct {
	Name       string
	Dimensions int
}

type Options struct {
	Query bool
}

// Embedder returns finite unit vectors in input order, in the space identified by Model.
// Query requests apply the model's retrieval instruction; documents remain unprefixed.
type Embedder interface {
	Model() Model
	Embed(context.Context, []string, *Options) ([][]float32, error)
}
