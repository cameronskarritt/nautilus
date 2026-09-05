package llm

import (
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

// ClientRegistry maps models to their LLM client implementations.
type ClientRegistry struct {
	clients map[enums.Model]Client
}

// NewClientRegistry creates an empty ClientRegistry.
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{clients: make(map[enums.Model]Client)}
}

// Register adds a client for the given model.
func (c *ClientRegistry) Register(model enums.Model, client Client) {
	c.clients[model] = client
}

// Get returns the client registered for the given model.
func (c *ClientRegistry) Get(model enums.Model) (Client, error) {
	client, ok := c.clients[model]
	if !ok {
		return nil, errors.Errorf("no client registered for model: %s", model)
	}
	return client, nil
}
