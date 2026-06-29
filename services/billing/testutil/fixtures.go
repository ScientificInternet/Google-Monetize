package testutil

import (
	"time"
)

// Testuser_tokens represents a test user token record
type Testuser_tokens struct {
	UserID    string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Testtoken_transactions represents a test token transaction
type Testtoken_transactions struct {
	ID          string
	UserID      string
	Type        string
	Amount      int64
	Description string
	Status      string
	CreatedAt   time.Time
}

// Testsubscriptions represents a test subscription
type Testsubscriptions struct {
	ID        string
	UserID    string
	PlanID    string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTestuser_tokens creates a test user token with default values
func NewTestuser_tokens() *Testuser_tokens {
	now := time.Now()
	return &Testuser_tokens{
		UserID:    "test-user-1",
		Balance:   1000,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewTestuser_tokensWithBalance creates a test user token with specific balance
func NewTestuser_tokensWithBalance(balance int64) *Testuser_tokens {
	token := NewTestuser_tokens()
	token.Balance = balance
	return token
}

// NewTesttoken_transactions creates a test token transaction
func NewTesttoken_transactions() *Testtoken_transactions {
	return &Testtoken_transactions{
		ID:          "test-tx-1",
		UserID:      "test-user-1",
		Type:        "deduct",
		Amount:      -100,
		Description: "Test transaction",
		Status:      "committed",
		CreatedAt:   time.Now(),
	}
}

// NewTestReservedTransaction creates a test reserved transaction
func NewTestReservedTransaction() *Testtoken_transactions {
	tx := NewTesttoken_transactions()
	tx.Status = "reserved"
	return tx
}

// NewTestCommittedTransaction creates a test committed transaction
func NewTestCommittedTransaction() *Testtoken_transactions {
	tx := NewTesttoken_transactions()
	tx.Status = "committed"
	return tx
}

// NewTestReleasedTransaction creates a test released transaction
func NewTestReleasedTransaction() *Testtoken_transactions {
	tx := NewTesttoken_transactions()
	tx.Status = "released"
	tx.Amount = 0
	return tx
}

// NewTestsubscriptions creates a test subscription
func NewTestsubscriptions() *Testsubscriptions {
	now := time.Now()
	return &Testsubscriptions{
		ID:        "test-sub-1",
		UserID:    "test-user-1",
		PlanID:    "plan-basic",
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewTestsubscriptionsWithStatus creates a test subscription with specific status
func NewTestsubscriptionsWithStatus(status string) *Testsubscriptions {
	sub := NewTestsubscriptions()
	sub.Status = status
	return sub
}

// Testuser_tokensBuilder provides a fluent interface for building test user tokens
type Testuser_tokensBuilder struct {
	token *Testuser_tokens
}

// Newuser_tokensBuilder creates a new Testuser_tokensBuilder
func Newuser_tokensBuilder() *Testuser_tokensBuilder {
	return &Testuser_tokensBuilder{
		token: NewTestuser_tokens(),
	}
}

// WithUserID sets the user ID
func (b *Testuser_tokensBuilder) WithUserID(userID string) *Testuser_tokensBuilder {
	b.token.UserID = userID
	return b
}

// WithBalance sets the balance
func (b *Testuser_tokensBuilder) WithBalance(balance int64) *Testuser_tokensBuilder {
	b.token.Balance = balance
	return b
}

// Build returns the built user token
func (b *Testuser_tokensBuilder) Build() *Testuser_tokens {
	return b.token
}

// Testtoken_transactionsBuilder provides a fluent interface for building test transactions
type Testtoken_transactionsBuilder struct {
	tx *Testtoken_transactions
}

// Newtoken_transactionsBuilder creates a new Testtoken_transactionsBuilder
func Newtoken_transactionsBuilder() *Testtoken_transactionsBuilder {
	return &Testtoken_transactionsBuilder{
		tx: NewTesttoken_transactions(),
	}
}

// WithID sets the transaction ID
func (b *Testtoken_transactionsBuilder) WithID(id string) *Testtoken_transactionsBuilder {
	b.tx.ID = id
	return b
}

// WithUserID sets the user ID
func (b *Testtoken_transactionsBuilder) WithUserID(userID string) *Testtoken_transactionsBuilder {
	b.tx.UserID = userID
	return b
}

// WithType sets the transaction type
func (b *Testtoken_transactionsBuilder) WithType(txType string) *Testtoken_transactionsBuilder {
	b.tx.Type = txType
	return b
}

// WithAmount sets the amount
func (b *Testtoken_transactionsBuilder) WithAmount(amount int64) *Testtoken_transactionsBuilder {
	b.tx.Amount = amount
	return b
}

// WithStatus sets the status
func (b *Testtoken_transactionsBuilder) WithStatus(status string) *Testtoken_transactionsBuilder {
	b.tx.Status = status
	return b
}

// WithDescription sets the description
func (b *Testtoken_transactionsBuilder) WithDescription(desc string) *Testtoken_transactionsBuilder {
	b.tx.Description = desc
	return b
}

// Build returns the built transaction
func (b *Testtoken_transactionsBuilder) Build() *Testtoken_transactions {
	return b.tx
}
