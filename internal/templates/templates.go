// Package templates keeps the seller's eBay listing templates: the ones
// FireCardManager on their PC sends (its Listing Templates) and ones made
// on the website. The Bulk Lister fills one in for each eBay listing.
package templates

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	MaxPerAccount = 50
	maxHTML       = 200_000
)

var (
	ErrNotFound = errors.New("template not found")
	ErrReadOnly = errors.New("templates from your PC are edited in FireCardManager")
)

type Template struct {
	ID              string    `json:"id"`
	Edition         string    `json:"edition"`
	Source          string    `json:"source"` // pc or web
	Name            string    `json:"name"`
	TitleTemplate   string    `json:"title_template"`
	DescriptionHTML string    `json:"description_html"`
	IsDefault       bool      `json:"is_default"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Service struct{ DB *sql.DB }

// Problem says what's wrong with a template ("" when fine).
func Problem(t Template) string {
	switch {
	case strings.TrimSpace(t.Name) == "" || len(t.Name) > 80:
		return "Give the template a name (80 characters at most)."
	case strings.TrimSpace(t.TitleTemplate) == "" || len(t.TitleTemplate) > 300:
		return "Enter a title pattern, like {Year} {Brand} {SetName} {PlayerName} #{CardNumber}."
	case strings.TrimSpace(t.DescriptionHTML) == "" || len(t.DescriptionHTML) > maxHTML:
		return "Enter the description (up to 200,000 characters)."
	}
	return ""
}

// ReplaceFromPC swaps in the PC's templates for its edition (the website's own stay).
func (s *Service) ReplaceFromPC(ctx context.Context, accountID, edition string, list []Template) error {
	if len(list) > MaxPerAccount {
		list = list[:MaxPerAccount]
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM listing_templates WHERE account_id = $1 AND edition = $2 AND source = 'pc'`, accountID, edition); err != nil {
		return err
	}
	for _, t := range list {
		if Problem(t) != "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO listing_templates (account_id, edition, source, name, title_template, description_html, is_default)
			VALUES ($1, $2, 'pc', $3, $4, $5, $6)`, accountID, edition, strings.TrimSpace(t.Name), t.TitleTemplate, t.DescriptionHTML, t.IsDefault); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const cols = `id, edition, source, name, title_template, description_html, is_default, updated_at`

func scan(r interface{ Scan(...any) error }) (Template, error) {
	var t Template
	err := r.Scan(&t.ID, &t.Edition, &t.Source, &t.Name, &t.TitleTemplate, &t.DescriptionHTML, &t.IsDefault, &t.UpdatedAt)
	return t, err
}

// List is the edition's templates, the default first.
func (s *Service) List(ctx context.Context, accountID, edition string) ([]Template, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM listing_templates WHERE account_id = $1 AND edition = $2
		ORDER BY is_default DESC, source, lower(name)`, accountID, edition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get is one template ("" gets the default, or nil when there are none).
func (s *Service) Get(ctx context.Context, accountID, edition, id string) (*Template, error) {
	var t Template
	var err error
	if id == "" {
		t, err = scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM listing_templates WHERE account_id = $1 AND edition = $2
			ORDER BY is_default DESC, source, updated_at DESC LIMIT 1`, accountID, edition))
	} else {
		t, err = scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM listing_templates WHERE account_id = $1 AND id::text = $2`, accountID, strings.ToLower(id)))
	}
	if errors.Is(err, sql.ErrNoRows) {
		if id == "" {
			return nil, nil
		}
		return nil, ErrNotFound
	}
	return &t, err
}

// Save creates (empty id) or changes a website template.
func (s *Service) Save(ctx context.Context, accountID string, t Template) (Template, error) {
	if t.ID == "" {
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM listing_templates WHERE account_id = $1`, accountID).Scan(&n); err != nil {
			return t, err
		}
		if n >= MaxPerAccount {
			return t, errors.New("too many templates")
		}
		return scan(s.DB.QueryRowContext(ctx, `INSERT INTO listing_templates (account_id, edition, source, name, title_template, description_html)
			VALUES ($1, $2, 'web', $3, $4, $5) RETURNING `+cols, accountID, t.Edition, strings.TrimSpace(t.Name), t.TitleTemplate, t.DescriptionHTML))
	}
	cur, err := s.Get(ctx, accountID, "", t.ID)
	if err != nil {
		return t, err
	}
	if cur.Source == "pc" {
		return t, ErrReadOnly
	}
	return scan(s.DB.QueryRowContext(ctx, `UPDATE listing_templates SET name = $3, title_template = $4, description_html = $5, updated_at = now()
		WHERE account_id = $1 AND id::text = $2 RETURNING `+cols, accountID, strings.ToLower(t.ID), strings.TrimSpace(t.Name), t.TitleTemplate, t.DescriptionHTML))
}

// Delete removes a website template.
func (s *Service) Delete(ctx context.Context, accountID, id string) error {
	cur, err := s.Get(ctx, accountID, "", id)
	if err != nil {
		return err
	}
	if cur.Source == "pc" {
		return ErrReadOnly
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM listing_templates WHERE account_id = $1 AND id::text = $2`, accountID, strings.ToLower(id))
	return err
}
