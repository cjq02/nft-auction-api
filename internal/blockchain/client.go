package blockchain

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
)

type Client struct {
	*ethclient.Client
	rpcURL string
}

func NewClient(rpcURL string) (*Client, error) {
	if rpcURL == "" {
		return nil, nil
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	return &Client{
		Client: client,
		rpcURL: rpcURL,
	}, nil
}

func (c *Client) Close() {
	if c != nil && c.Client != nil {
		c.Client.Close()
	}
}

func (c *Client) IsAvailable() bool {
	return c != nil && c.Client != nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("blockchain client not configured")
	}
	_, err := c.ChainID(ctx)
	return err
}
