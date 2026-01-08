// Package api provides the HTTP API for dispatchoor.
//
//	@title						Dispatchoor API
//	@version					1.0
//	@description				GitHub Actions workflow dispatch queue management API.
//	@description				Dispatchoor helps you manage and schedule GitHub Actions workflow dispatches
//	@description				across multiple runner pools with fine-grained control.
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
