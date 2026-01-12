// Package api provides the HTTP API for dispatchoor.
//
//	@title						Dispatchoor API
//	@version					1.0
//	@description				GitHub Actions workflow dispatch queue management API.
//	@description				Dispatchoor helps you manage and schedule GitHub Actions workflow dispatches
//	@description				across multiple runner pools with fine-grained control.
//	@description
//	@description				## Rate Limiting
//	@description				When enabled, the API enforces per-IP rate limits on three tiers:
//	@description				- **Auth endpoints** (`/auth/*`): 10 requests/minute (protects against brute force)
//	@description				- **Public endpoints** (`/health`, `/metrics`): 60 requests/minute
//	@description				- **Authenticated endpoints**: 120 requests/minute
//	@description
//	@description				When rate limited, the API returns HTTP 429 with a `Retry-After` header.
//
//	@contact.name				ethPandaOps
//	@contact.url				https://github.com/ethpandaops/dispatchoor
//
//	@license.name				MIT
//	@license.url				https://github.com/ethpandaops/dispatchoor/blob/main/LICENSE
//
//	@host						localhost:9090
//	@BasePath					/api/v1
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Bearer token authentication. Format: "Bearer {token}"
//
//	@tag.name					auth
//	@tag.description			Authentication endpoints
//
//	@tag.name					groups
//	@tag.description			Runner group management
//
//	@tag.name					templates
//	@tag.description			Job template management
//
//	@tag.name					queue
//	@tag.description			Job queue operations
//
//	@tag.name					jobs
//	@tag.description			Job management
//
//	@tag.name					history
//	@tag.description			Job history and statistics
//
//	@tag.name					runners
//	@tag.description			GitHub Actions runner information
//
//	@tag.name					system
//	@tag.description			System health and status
//
//	@tag.name					websocket
//	@tag.description			Real-time event streaming
package api
