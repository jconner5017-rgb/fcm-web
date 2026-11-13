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

// OptIn turns on business policies for the seller (already on is fine).
func (s *Service) OptIn(ctx context.Context, token string) error {
	err := s.call(ctx, token, http.MethodPost, "/sell/account/v1/program/opt_in", map[string]string{"programType": "SELLING_POLICY_MANAGEMENT"}, nil)
	if e, ok := err.(*APIError); ok && (e.Status == 409 || strings.Contains(e.Body, "20401")) {
		return nil
	}
	return err
}

// Profile is a FireCardManager shipping profile (ShippingProfile.cs): the
// package and how it ships. A listing's eBay shipping policy is made from
// it, named the way the desktop names it, so both apps share policies.
type Profile struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Package      string  `json:"package"` // Envelope, BubbleMailer, RigidMailer, Box
	LengthIn     float64 `json:"length_in"`
	WidthIn      float64 `json:"width_in"`
	HeightIn     float64 `json:"height_in"`
	WeightOz     float64 `json:"weight_oz"`
	CarrierCode  string  `json:"carrier_code"`
	ServiceCode  string  `json:"service_code"`
	HandlingDays int     `json:"handling_days"`
	Cost         string  `json:"cost"` // EbayRate, Free, Flat
	FlatAmount   float64 `json:"flat_amount"`
}

// DefaultProfiles are the three most sellers start with (the desktop's).
func DefaultProfiles() []Profile {
	return []Profile{
		{ID: "raw", Name: "Raw", Package: "Envelope", WeightOz: 1, CarrierCode: "USPS", ServiceCode: "US_eBayStandardEnvelope", HandlingDays: 1, Cost: "EbayRate"},
		{ID: "graded", Name: "Graded", Package: "BubbleMailer", LengthIn: 8, WidthIn: 6, HeightIn: 1, WeightOz: 3, CarrierCode: "USPS", ServiceCode: "USPSParcel", HandlingDays: 1, Cost: "EbayRate"},
		{ID: "oversize", Name: "Oversize", Package: "RigidMailer", LengthIn: 10, WidthIn: 6, HeightIn: 0.25, WeightOz: 2, CarrierCode: "USPS", ServiceCode: "USPSParcel", HandlingDays: 1, Cost: "EbayRate"},
	}
}

func (p Profile) IsEnvelope() bool { return p.Package == "Envelope" }

// EnvelopeRate is eBay Standard Envelope's price by packed weight (0 over 3 oz).
func EnvelopeRate(oz float64) float64 {
	switch {
	case oz <= 0:
		return 0
	case oz <= 1:
		return 0.78
	case oz <= 2:
		return 1.07
	case oz <= 3:
		return 1.36
	}
	return 0
}

// PackageType is eBay's PackageTypeEnum for the profile.
func (p Profile) PackageType() string {
	switch p.Package {
	case "Envelope":
		return "LETTER"
	case "RigidMailer":
		return "LARGE_ENVELOPE"
	case "Box":
		return "MAILING_BOX"
	}
	return "PACKAGE_THICK_ENVELOPE"
}

// Problem is what's wrong with the profile ("" when usable).
func (p Profile) Problem() string {
	switch {
	case strings.TrimSpace(p.Name) == "":
		return "Give the profile a name."
	case p.WeightOz <= 0:
		return "Enter the packed weight."
	case p.IsEnvelope() && p.ServiceCode != "US_eBayStandardEnvelope":
		return "Envelopes ship with eBay Standard Envelope."
	case p.IsEnvelope() && p.WeightOz > 3:
		return "eBay Standard Envelope takes up to 3 oz."
	case !p.IsEnvelope() && (p.LengthIn <= 0 || p.WidthIn <= 0 || p.HeightIn <= 0):
		return "Enter the package length, width and height."
	case !p.IsEnvelope() && p.ServiceCode == "US_eBayStandardEnvelope":
		return "eBay Standard Envelope is for envelopes only; pick another service."
	case p.Cost == "Flat" && p.FlatAmount <= 0:
		return "Enter what the buyer pays for shipping."
	case p.HandlingDays < 1 || p.HandlingDays > 3:
		return "Handling time is 1 to 3 business days."
	}
	return ""
}

// PackageDetails are the inventory item's package weight and size.
func (p Profile) PackageDetails() map[string]any {
	d := map[string]any{"weight": map[string]any{"value": p.WeightOz, "unit": "OUNCE"}, "packageType": p.PackageType()}
	if !p.IsEnvelope() && p.LengthIn > 0 && p.WidthIn > 0 && p.HeightIn > 0 {
		d["dimensions"] = map[string]any{"length": p.LengthIn, "width": p.WidthIn, "height": p.HeightIn, "unit": "INCH"}
	}
	return d
}

func policyName(suffix string) string {
	n := "FireCardManager - " + suffix
	if len(n) > 60 {
		n = n[:60]
	}
	return n
}

// Policies for one listing run: shipping from the profile, payment and
// returns. Each is found by its FireCardManager name, or created - the same
// names the desktop uses, so policies it made are reused.
func (s *Service) ResolvePolicies(ctx context.Context, token string, p Profile, returns30 bool) (shipID, payID, retID string, err error) {
	if prob := p.Problem(); prob != "" {
		return "", "", "", fmt.Errorf("shipping profile %q: %s", p.Name, prob)
	}
	if err = s.OptIn(ctx, token); err != nil {
		return
	}
	shipList, payList, retList, err := s.Policies(ctx, token)
	if err != nil {
		return
	}
	find := func(list []Policy, name string) string {
		for _, x := range list {
			if x.Name == name {
				return x.ID
			}
		}
		return ""
	}
	create := func(kind, idKey string, body map[string]any) (string, error) {
		var out map[string]any
		if err := s.call(ctx, token, http.MethodPost, "/sell/account/v1/"+kind, body, &out); err != nil {
			return "", err
		}
		id, _ := out[idKey].(string)
		return id, nil
	}
	cats := []map[string]string{{"name": "ALL_EXCLUDING_MOTORS_VEHICLES"}}

	name := policyName("Payment")
	if payID = find(payList, name); payID == "" {
		if payID, err = create("payment_policy", "paymentPolicyId", map[string]any{"name": name, "marketplaceId": Marketplace,
			"categoryTypes": cats, "immediatePay": false}); err != nil {
			return
		}
	}
	retBody := map[string]any{"marketplaceId": Marketplace, "returnsAccepted": false}
	name = policyName("NoReturns")
	if returns30 {
		name = policyName("30 Day Returns")
		retBody = map[string]any{"marketplaceId": Marketplace, "returnsAccepted": true, "returnPeriod": map[string]any{"value": 30, "unit": "DAY"},
			"returnShippingCostPayer": "BUYER", "refundMethod": "MONEY_BACK"}
	}
	retBody["name"] = name
	if retID = find(retList, name); retID == "" {
		if retID, err = create("return_policy", "returnPolicyId", retBody); err != nil {
			return
		}
	}

	// Shipping, as ResolveProfilePolicyIdAsync builds it.
	svc := map[string]any{"sortOrder": 1, "shippingCarrierCode": p.CarrierCode, "shippingServiceCode": p.ServiceCode}
	costType, label := "FLAT_RATE", ""
	amount := 0.0
	switch {
	case p.Cost == "Free":
		svc["freeShipping"], label = true, "Free"
	case p.Cost == "Flat" || p.IsEnvelope():
		amount = p.FlatAmount
		if p.Cost != "Flat" {
			amount = EnvelopeRate(p.WeightOz)
		}
		svc["freeShipping"], svc["buyerResponsibleForShipping"] = false, true
		svc["shippingCost"] = map[string]string{"currency": "USD", "value": fmt.Sprintf("%.2f", amount)}
		label = fmt.Sprintf("%.2f", amount)
	default:
		svc["freeShipping"], svc["buyerResponsibleForShipping"], costType, label = false, true, "CALCULATED", "Calc"
	}
	name = policyName(fmt.Sprintf("%s - %s - %s - %dd", p.Name, p.ServiceCode, label, p.HandlingDays))
	if shipID = find(shipList, name); shipID == "" {
		shipID, err = create("fulfillment_policy", "fulfillmentPolicyId", map[string]any{"name": name, "marketplaceId": Marketplace,
			"categoryTypes": cats, "handlingTime": map[string]any{"value": p.HandlingDays, "unit": "DAY"},
			"shippingOptions": []map[string]any{{"optionType": "DOMESTIC", "costType": costType, "shippingServices": []map[string]any{svc}}}})
	}
	return
}
