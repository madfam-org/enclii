package billing

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/invoice"
	"github.com/stripe/stripe-go/v76/invoiceitem"
	"github.com/stripe/stripe-go/v76/subscription"
	"go.uber.org/zap"
)

// Deprecated: StripeClient is superseded by Dhanam billing SDK.
// Checkout, subscription, and invoice operations are now handled by Dhanam.
// This client remains for backwards compatibility during the migration period.
// Remove once all billing flows are verified through Dhanam.
// StripeClient handles Stripe integration
type StripeClient struct {
	logger *zap.Logger
}

// NewStripeClient creates a new Stripe client
func NewStripeClient(apiKey string, logger *zap.Logger) *StripeClient {
	stripe.Key = apiKey
	return &StripeClient{
		logger: logger,
	}
}

// CreateCustomer creates a Stripe customer for a project/team
func (s *StripeClient) CreateCustomer(ctx context.Context, email, name string, projectID uuid.UUID) (*stripe.Customer, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
		Metadata: map[string]string{
			"project_id": projectID.String(),
		},
	}

	cust, err := customer.New(params)
	if err != nil {
		return nil, fmt.Errorf("failed to create customer: %w", err)
	}

	s.logger.Info("stripe customer created",
		zap.String("customer_id", cust.ID),
		zap.String("project_id", projectID.String()),
	)

	return cust, nil
}

// CreateSubscription creates a subscription for a customer
func (s *StripeClient) CreateSubscription(ctx context.Context, customerID, priceID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{
				Price: stripe.String(priceID),
			},
		},
	}

	sub, err := subscription.New(params)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	s.logger.Info("stripe subscription created",
		zap.String("subscription_id", sub.ID),
		zap.String("customer_id", customerID),
	)

	return sub, nil
}

// CancelSubscription cancels a subscription
func (s *StripeClient) CancelSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionCancelParams{}

	sub, err := subscription.Cancel(subscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel subscription: %w", err)
	}

	s.logger.Info("stripe subscription cancelled",
		zap.String("subscription_id", subscriptionID),
	)

	return sub, nil
}

// CreateUsageInvoice creates an invoice for usage charges
func (s *StripeClient) CreateUsageInvoice(ctx context.Context, customerID string, items []*UsageLineItem) (*stripe.Invoice, error) {
	// Add invoice items
	for _, item := range items {
		itemParams := &stripe.InvoiceItemParams{
			Customer:    stripe.String(customerID),
			Amount:      stripe.Int64(int64(item.AmountCents)),
			Currency:    stripe.String("usd"),
			Description: stripe.String(item.Description),
			Metadata: map[string]string{
				"metric_type": item.MetricType,
				"quantity":    fmt.Sprintf("%.4f", item.Quantity),
				"unit_price":  fmt.Sprintf("%.6f", item.UnitPrice),
			},
		}

		_, err := invoiceitem.New(itemParams)
		if err != nil {
			return nil, fmt.Errorf("failed to create invoice item: %w", err)
		}
	}

	// Create and finalize invoice
	invoiceParams := &stripe.InvoiceParams{
		Customer:         stripe.String(customerID),
		AutoAdvance:      stripe.Bool(true),
		CollectionMethod: stripe.String("charge_automatically"),
	}

	inv, err := invoice.New(invoiceParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}

	s.logger.Info("stripe invoice created",
		zap.String("invoice_id", inv.ID),
		zap.String("customer_id", customerID),
		zap.Int("items", len(items)),
	)

	return inv, nil
}

// UsageLineItem represents a line item for usage billing
type UsageLineItem struct {
	MetricType  string  `json:"metric_type"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	AmountCents int     `json:"amount_cents"`
}

// GetCustomer retrieves a Stripe customer
func (s *StripeClient) GetCustomer(ctx context.Context, customerID string) (*stripe.Customer, error) {
	cust, err := customer.Get(customerID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer: %w", err)
	}
	return cust, nil
}

// GetSubscription retrieves a Stripe subscription
func (s *StripeClient) GetSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	sub, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}
	return sub, nil
}

// GetInvoice retrieves a Stripe invoice
func (s *StripeClient) GetInvoice(ctx context.Context, invoiceID string) (*stripe.Invoice, error) {
	inv, err := invoice.Get(invoiceID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get invoice: %w", err)
	}
	return inv, nil
}

// ListInvoices lists invoices for a customer
func (s *StripeClient) ListInvoices(ctx context.Context, customerID string, limit int) ([]*stripe.Invoice, error) {
	params := &stripe.InvoiceListParams{
		Customer: stripe.String(customerID),
	}
	params.Limit = stripe.Int64(int64(limit))

	var invoices []*stripe.Invoice
	i := invoice.List(params)
	for i.Next() {
		invoices = append(invoices, i.Invoice())
	}

	if err := i.Err(); err != nil {
		return nil, fmt.Errorf("failed to list invoices: %w", err)
	}

	return invoices, nil
}

// IsEnabled returns true if the Stripe API key is configured.
func (s *StripeClient) IsEnabled() bool {
	return stripe.Key != ""
}

// GetCustomerIDForProject looks up the Stripe customer associated with a project
// by searching customer metadata. Returns empty string if no customer is found.
func (s *StripeClient) GetCustomerIDForProject(ctx context.Context, projectID string) (string, error) {
	params := &stripe.CustomerSearchParams{
		SearchParams: stripe.SearchParams{
			Query: fmt.Sprintf("metadata['project_id']:'%s'", projectID),
		},
	}
	iter := customer.Search(params)
	if iter.Next() {
		return iter.Customer().ID, nil
	}
	if err := iter.Err(); err != nil {
		return "", fmt.Errorf("stripe customer search: %w", err)
	}
	return "", nil
}
