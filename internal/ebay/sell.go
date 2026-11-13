package ebay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Marketplace is the eBay site listings go on (FireCardManager lists on eBay US).
const Marketplace = "EBAY_US"

// APIError is eBay turning a call down; Message is eBay's own first message.
type APIError struct {
	Status  int
	Message string
	Body    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return "eBay: " + e.Message
	}
	return fmt.Sprintf("eBay error %d", e.Status)
}

// call makes one Sell API call with the seller's token.
func (s *Service) call(ctx context.Context, token, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.APIBase()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Language", "en-US")
	}
	res, err := s.client().Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach eBay: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Errors []struct {
				Message string `json:"message"`
				LongMsg string `json:"longMessage"`
				ErrorID int    `json:"errorId"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(b, &e)
		msg := ""
		if len(e.Errors) > 0 {
			msg = e.Errors[0].LongMsg
			if msg == "" {
				msg = e.Errors[0].Message
			}
		}
		return &APIError{Status: res.StatusCode, Message: msg, Body: string(b)}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// Policy is one of the seller's business policies.
type Policy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Policies lists the seller's shipping, payment and return policies.
func (s *Service) Policies(ctx context.Context, token string) (ship, pay, ret []Policy, err error) {
	get := func(kind, listKey, idKey string) ([]Policy, error) {
		var raw map[string]json.RawMessage
		if err := s.call(ctx, token, http.MethodGet, "/sell/account/v1/"+kind+"?marketplace_id="+Marketplace, nil, &raw); err != nil {
			return nil, err
		}
		var items []map[string]any
		_ = json.Unmarshal(raw[listKey], &items)
		out := []Policy{}
		for _, it := range items {
			id, _ := it[idKey].(string)
			name, _ := it["name"].(string)
			if id != "" {
				out = append(out, Policy{ID: id, Name: name})
			}
		}
		return out, nil
	}
	if ship, err = get("fulfillment_policy", "fulfillmentPolicies", "fulfillmentPolicyId"); err != nil {
		return
	}
	if pay, err = get("payment_policy", "paymentPolicies", "paymentPolicyId"); err != nil {
		return
	}
	ret, err = get("return_policy", "returnPolicies", "returnPolicyId")
	return
}

// Location is a ship-from location (inventory location).
type Location struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	City       string `json:"city,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
}

// Locations lists the seller's inventory locations.
func (s *Service) Locations(ctx context.Context, token string) ([]Location, error) {
	var raw struct {
		Locations []struct {
			Key      string `json:"merchantLocationKey"`
			Name     string `json:"name"`
			Location struct {
				Address struct {
					City       string `json:"city"`
					PostalCode string `json:"postalCode"`
				} `json:"address"`
			} `json:"location"`
		} `json:"locations"`
	}
	if err := s.call(ctx, token, http.MethodGet, "/sell/inventory/v1/location?limit=100", nil, &raw); err != nil {
		return nil, err
	}
	out := []Location{}
	for _, l := range raw.Locations {
		out = append(out, Location{Key: l.Key, Name: l.Name, City: l.Location.Address.City, PostalCode: l.Location.Address.PostalCode})
	}
	return out, nil
}

// CreateLocation adds a ship-from location by ZIP code (an existing key is fine).
func (s *Service) CreateLocation(ctx context.Context, token, key, postalCode string) error {
	body := map[string]any{
		"location":               map[string]any{"address": map[string]string{"postalCode": postalCode, "country": "US"}},
		"locationTypes":          []string{"WAREHOUSE"},
		"name":                   "FireCardManager Ship-From Location",
		"merchantLocationStatus": "ENABLED",
	}
	err := s.call(ctx, token, http.MethodPost, "/sell/inventory/v1/location/"+url.PathEscape(key), body, nil)
	if e, ok := err.(*APIError); ok && (e.Status == 409 || strings.Contains(strings.ToLower(e.Body), "already exists")) {
		return nil
	}
	return err
}

// PutItem creates or replaces the inventory item for a SKU.
func (s *Service) PutItem(ctx context.Context, token, sku string, item map[string]any) error {
	return s.call(ctx, token, http.MethodPut, "/sell/inventory/v1/inventory_item/"+url.PathEscape(sku), item, nil)
}

// existingOffer reads the offer id from eBay's "offer already exists" error.
func existingOffer(e *APIError) string {
	var body struct {
		Errors []struct {
			Message    string `json:"message"`
			Parameters []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"parameters"`
		} `json:"errors"`
	}
	_ = json.Unmarshal([]byte(e.Body), &body)
	for _, er := range body.Errors {
		if !strings.Contains(strings.ToLower(er.Message), "already exists") {
			continue
		}
		for _, p := range er.Parameters {
			if p.Name == "offerId" {
				return p.Value
			}
		}
	}
	return ""
}

// Offer creates the offer for a SKU, or replaces the one already there.
func (s *Service) Offer(ctx context.Context, token string, offer map[string]any) (string, error) {
	var out struct {
		OfferID string `json:"offerId"`
	}
	err := s.call(ctx, token, http.MethodPost, "/sell/inventory/v1/offer", offer, &out)
	if e, ok := err.(*APIError); ok {
		if id := existingOffer(e); id != "" {
			return id, s.call(ctx, token, http.MethodPut, "/sell/inventory/v1/offer/"+url.PathEscape(id), offer, nil)
		}
	}
	return out.OfferID, err
}

// Publish makes the offer a live listing; returns the listing id.
func (s *Service) Publish(ctx context.Context, token, offerID string) (string, error) {
	var out struct {
		ListingID string `json:"listingId"`
	}
	err := s.call(ctx, token, http.MethodPost, "/sell/inventory/v1/offer/"+url.PathEscape(offerID)+"/publish", nil, &out)
	if out.ListingID == "" && err == nil {
		out.ListingID = offerID
	}
	return out.ListingID, err
}

// Withdraw ends the listing (the inventory item and offer stay, so it can be relisted).
func (s *Service) Withdraw(ctx context.Context, token, offerID string) error {
	err := s.call(ctx, token, http.MethodPost, "/sell/inventory/v1/offer/"+url.PathEscape(offerID)+"/withdraw", nil, nil)
	if e, ok := err.(*APIError); ok && (e.Status == 404 || strings.Contains(strings.ToLower(e.Body), "not published")) {
		return nil
	}
	return err
}

// SetPrice changes a live fixed-price listing's price.
func (s *Service) SetPrice(ctx context.Context, token, sku, offerID string, price float64) error {
	body := map[string]any{"requests": []map[string]any{{
		"sku": sku,
		"offers": []map[string]any{{"offerId": offerID, "availableQuantity": 1,
			"price": map[string]string{"value": fmt.Sprintf("%.2f", price), "currency": "USD"}}},
	}}}
	var out struct {
		Responses []struct {
			StatusCode int `json:"statusCode"`
			Errors     []struct {
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"responses"`
	}
	if err := s.call(ctx, token, http.MethodPost, "/sell/inventory/v1/bulk_update_price_quantity", body, &out); err != nil {
		return err
	}
	for _, r := range out.Responses {
		if r.StatusCode >= 300 {
			msg := ""
			if len(r.Errors) > 0 {
				msg = r.Errors[0].Message
			}
			return &APIError{Status: r.StatusCode, Message: msg}
		}
	}
	return nil
}
