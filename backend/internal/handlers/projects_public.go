package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/jagadeesh/grainlify/backend/internal/db"
)

type ProjectsPublicHandler struct {
	db *db.DB
}

func NewProjectsPublicHandler(d *db.DB) *ProjectsPublicHandler {
	return &ProjectsPublicHandler{db: d}
}

// List returns a filtered list of verified projects.
// Query parameters:
//   - ecosystem: filter by ecosystem name (case-insensitive)
//   - language: filter by programming language
//   - category: filter by category
//   - tags: comma-separated list of tags (project must have ALL tags)
//   - limit: max results (default 50, max 200)
//   - offset: pagination offset (default 0)
func (h *ProjectsPublicHandler) List() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.db == nil || h.db.Pool == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "db_not_configured"})
		}

		// Parse query parameters
		ecosystem := strings.TrimSpace(c.Query("ecosystem"))
		language := strings.TrimSpace(c.Query("language"))
		category := strings.TrimSpace(c.Query("category"))
		tagsParam := strings.TrimSpace(c.Query("tags"))
		
		limit := 50
		if l := c.QueryInt("limit", 50); l > 0 && l <= 200 {
			limit = l
		}
		offset := c.QueryInt("offset", 0)
		if offset < 0 {
			offset = 0
		}

		// Build WHERE clause and args
		var conditions []string
		var args []any
		argPos := 1

		// Only show verified projects
		conditions = append(conditions, "p.status = 'verified'")

		// Filter by ecosystem
		if ecosystem != "" {
			conditions = append(conditions, fmt.Sprintf("LOWER(TRIM(e.name)) = LOWER($%d)", argPos))
			args = append(args, ecosystem)
			argPos++
		}

		// Filter by language
		if language != "" {
			conditions = append(conditions, fmt.Sprintf("LOWER(TRIM(p.language)) = LOWER($%d)", argPos))
			args = append(args, language)
			argPos++
		}

		// Filter by category
		if category != "" {
			conditions = append(conditions, fmt.Sprintf("LOWER(TRIM(p.category)) = LOWER($%d)", argPos))
			args = append(args, category)
			argPos++
		}

		// Filter by tags (must have ALL specified tags)
		var tags []string
		if tagsParam != "" {
			for _, tag := range strings.Split(tagsParam, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tags = append(tags, tag)
				}
			}
		}
		if len(tags) > 0 {
			// Use JSONB containment operator @> to check if tags array contains all specified tags
			conditions = append(conditions, fmt.Sprintf("p.tags @> $%d::jsonb", argPos))
			tagsJSON, _ := json.Marshal(tags)
			args = append(args, string(tagsJSON))
			argPos++
		}

		whereClause := strings.Join(conditions, " AND ")

		// Build query
		query := fmt.Sprintf(`
SELECT 
  p.id,
  p.github_full_name,
  p.language,
  p.tags,
  p.category,
  p.stars_count,
  p.forks_count,
  p.created_at,
  p.updated_at,
  e.name AS ecosystem_name,
  e.slug AS ecosystem_slug
FROM projects p
LEFT JOIN ecosystems e ON p.ecosystem_id = e.id
WHERE %s
ORDER BY p.created_at DESC
LIMIT $%d OFFSET $%d
`, whereClause, argPos, argPos+1)
		args = append(args, limit, offset)

		rows, err := h.db.Pool.Query(c.Context(), query, args...)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "projects_list_failed"})
		}
		defer rows.Close()

		var out []fiber.Map
		for rows.Next() {
			var id uuid.UUID
			var fullName string
			var language, category *string
			var tagsJSON []byte
			var starsCount, forksCount *int
			var createdAt, updatedAt time.Time
			var ecosystemName, ecosystemSlug *string

			if err := rows.Scan(&id, &fullName, &language, &tagsJSON, &category, &starsCount, &forksCount, &createdAt, &updatedAt, &ecosystemName, &ecosystemSlug); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "projects_list_failed", "details": err.Error()})
			}

			// Parse tags JSONB
			var tags []string
			if len(tagsJSON) > 0 {
				_ = json.Unmarshal(tagsJSON, &tags)
			}

			// Default to 0 if nil
			stars := 0
			if starsCount != nil {
				stars = *starsCount
			}
			forks := 0
			if forksCount != nil {
				forks = *forksCount
			}

			out = append(out, fiber.Map{
				"id":              id.String(),
				"github_full_name": fullName,
				"language":        language,
				"tags":            tags,
				"category":        category,
				"stars_count":     stars,
				"forks_count":     forks,
				"ecosystem_name":  ecosystemName,
				"ecosystem_slug":  ecosystemSlug,
				"created_at":      createdAt,
				"updated_at":      updatedAt,
			})
		}

		// Get total count for pagination
		countQuery := fmt.Sprintf(`
SELECT COUNT(*)
FROM projects p
LEFT JOIN ecosystems e ON p.ecosystem_id = e.id
WHERE %s
`, whereClause)
		countArgs := args[:len(args)-2] // Remove limit and offset

		var total int
		if err := h.db.Pool.QueryRow(c.Context(), countQuery, countArgs...).Scan(&total); err != nil {
			// If count fails, just return results without total
			total = len(out)
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"projects": out,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}

// FilterOptions returns available filter values (languages, categories, tags) from verified projects.
func (h *ProjectsPublicHandler) FilterOptions() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.db == nil || h.db.Pool == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "db_not_configured"})
		}

		// Get distinct languages
		langRows, err := h.db.Pool.Query(c.Context(), `
SELECT DISTINCT language
FROM projects
WHERE status = 'verified' AND language IS NOT NULL AND language != ''
ORDER BY language
`)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "filter_options_failed"})
		}
		defer langRows.Close()

		var languages []string
		for langRows.Next() {
			var lang string
			if err := langRows.Scan(&lang); err == nil {
				languages = append(languages, lang)
			}
		}

		// Get distinct categories
		catRows, err := h.db.Pool.Query(c.Context(), `
SELECT DISTINCT category
FROM projects
WHERE status = 'verified' AND category IS NOT NULL AND category != ''
ORDER BY category
`)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "filter_options_failed"})
		}
		defer catRows.Close()

		var categories []string
		for catRows.Next() {
			var cat string
			if err := catRows.Scan(&cat); err == nil {
				categories = append(categories, cat)
			}
		}

		// Get all unique tags from verified projects
		tagRows, err := h.db.Pool.Query(c.Context(), `
SELECT DISTINCT jsonb_array_elements_text(tags) AS tag
FROM projects
WHERE status = 'verified' AND tags IS NOT NULL AND jsonb_array_length(tags) > 0
ORDER BY tag
`)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "filter_options_failed"})
		}
		defer tagRows.Close()

		tagMap := make(map[string]bool)
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err == nil && tag != "" {
				tagMap[tag] = true
			}
		}
		var tags []string
		for tag := range tagMap {
			tags = append(tags, tag)
		}
		// Sort tags
		for i := 0; i < len(tags)-1; i++ {
			for j := i + 1; j < len(tags); j++ {
				if tags[i] > tags[j] {
					tags[i], tags[j] = tags[j], tags[i]
				}
			}
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"languages": languages,
			"categories": categories,
			"tags":      tags,
		})
	}
}

