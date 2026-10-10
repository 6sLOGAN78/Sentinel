# Sentinel

## Overview

This is a production-ready Go backend system designed for building scalable and maintainable applications.

The purpose of Sentinel is to provide:

- Clean architecture
- Scalable project structure
- Production-ready tooling
- High performance
- Better developer experience
- Easy testing and deployment

Sentinel includes carefully selected tools and libraries commonly used in modern backend development.

---

# Tech Stack

| Purpose | Library / Tool |
|---|---|
| Web Server | Echo |
| Database Driver | pgx |
| Database | PostgreSQL |
| Logging | Zerolog |
| Config Management | Koanf / Viper |
| Validation | Validator |
| Database Migration | tern |
| Testing | Testify |
| Test Containers | Testcontainers |
| Package Manager / Tooling | Bun |

---

# 1. Web Server — Echo

## What is Echo?

Echo is a high-performance web framework for Go used to build APIs and backend services.

It helps manage:

- HTTP requests
- Routing
- Middleware
- Authentication
- JSON APIs
- Request handling

---

## Why We Use Echo

### 1. Very Fast

Echo is one of the fastest Go web frameworks.

Combined with Go, it provides excellent performance for APIs and backend systems.

---

### 2. Simple and Clean Routing

```go
e.GET("/users", getUsers)
```

Routes remain clean and easy to maintain.

---

### 3. Middleware Support

Echo supports middleware for:

- Authentication
- Logging
- JWT
- Rate limiting
- CORS

---

### 4. Production Ready

Echo is widely used in production-grade backend systems and microservices.

---

# 2. Database Driver — pgx

## What is pgx?

pgx is a PostgreSQL driver and toolkit for Go.

It allows Go applications to communicate efficiently with PostgreSQL.

---

## Why We Use pgx

### 1. Better Performance

pgx is faster than traditional PostgreSQL drivers for Go.

It provides:

- Faster queries
- Better connection handling
- Lower latency

---

### 2. Native PostgreSQL Features

Supports advanced PostgreSQL features like:

- JSONB
- COPY
- LISTEN / NOTIFY
- Prepared statements
- Transactions

---

### 3. Better Connection Pooling

Efficient connection pooling improves scalability and resource management.

---

# 3. Database — PostgreSQL

## What is PostgreSQL?

PostgreSQL is an open-source relational database system.

It is known for reliability, scalability, and advanced SQL support.

---

## Why We Use PostgreSQL

### 1. Reliability

PostgreSQL is highly stable and ACID compliant.

---

### 2. Advanced Features

Supports:

- JSON
- Transactions
- Indexing
- Full-text search
- Extensions

---

### 3. Scalability

Suitable for:

- Small applications
- Enterprise systems
- High-traffic APIs

---

# 4. Logging — Zerolog

## What is Zerolog?

Zerolog is a structured logging library for Go.

It produces JSON logs optimized for production systems.

---

## Why We Use Zerolog

### 1. Extremely Fast

Zerolog has very low overhead and excellent performance.

---

### 2. Structured Logging

Instead of plain text logs:

```txt
User login failed
```

We get structured logs:

```json
{
  "level":"error",
  "user_id":12,
  "message":"login failed"
}
```

This makes monitoring and debugging easier.

---

### 3. Cloud Native Friendly

Works well with:

- Docker
- Kubernetes
- Grafana
- Loki
- ELK Stack

---

# 5. Config Management — Koanf / Viper

## Why Configuration Management Matters

Applications need configurations such as:

- Database URLs
- Ports
- API keys
- Secrets
- Environment variables

Managing configurations properly is critical for maintainability.

---

# Why We Use Koanf

## 1. Modular Design

Koanf has a clean and modern architecture.

---

## 2. Multiple Config Sources

Supports:

- YAML
- JSON
- ENV variables
- Flags

---

## 3. Cleaner Configuration Structure

Helps organize configurations in large applications.

---

# Why We Use Viper in Bigger Applications

## 1. Large Ecosystem

Viper is widely used in enterprise Go applications.

---

## 2. Dynamic Config Reloading

Useful for large systems where configs may change during runtime.

---

# 6. Validation — Validator

## What is Validator?

Validator is used to validate incoming request data.

Example:

```go
type User struct {
    Email string `validate:"required,email"`
}
```

---

## Why We Use Validator

### 1. Cleaner APIs

Invalid requests are rejected early.

---

### 2. Better Security

Prevents malformed or dangerous data from entering the application.

---

### 3. Reduced Bugs

Ensures only valid data reaches business logic.

---

# 7. Database Migration — tern

## What are Database Migrations?

Database migrations manage schema changes over time.

Examples:

- Creating tables
- Adding columns
- Updating indexes

---

## Why We Use tern

### 1. Database Version Control

Tracks database schema changes safely.

---

### 2. Easier Team Collaboration

All developers use the same database structure.

---

### 3. Rollback Support

Allows reverting problematic schema updates.

---

# 8. Testing — Testify

## What is Testify?

Testify is a testing toolkit for Go.

It simplifies writing readable and maintainable tests.

---

## Why We Use Testify

### 1. Better Assertions

```go
assert.Equal(t, expected, got)
```

Cleaner than manual comparisons.

---

### 2. Better Readability

Tests become easier to understand and maintain.

---

### 3. Mocking Support

Supports mocking dependencies during testing.

---

# 9. Test Containers — Testcontainers

## What is Testcontainers?

Testcontainers allows running real services inside Docker containers during tests.

Examples:

- PostgreSQL
- Redis
- Kafka

---

## Why We Use Testcontainers

### 1. Realistic Testing

Tests run against actual services instead of mocks.

---

### 2. Isolated Environment

Each test gets a clean environment.

---

### 3. CI/CD Friendly

Works well in automated pipelines.

---

# 10. Package Manager & Tooling — Bun

## What is Bun?

Bun is a fast JavaScript runtime and package manager.

It can replace tools like:

- npm
- yarn
- pnpm

---

## Why We Use Bun

### 1. Extremely Fast

Bun is significantly faster than npm and yarn.

This improves:

- Dependency installation speed
- Script execution
- Development workflow

---

### 2. Better Developer Experience

Bun provides:

- Faster startup
- Simpler tooling
- Better performance

---

### 3. Modern Tooling Support

Useful for frontend tooling and development scripts commonly used alongside backend applications.

Examples:

- Tailwind
- Vite
- Frontend dashboards
- Admin panels
- Build scripts

---

### 4. All-in-One Tool

Bun includes:

- Package manager
- Runtime
- Bundler
- Test runner

Reducing dependency on multiple tools.

---

# Why We Use a Monorepo

## What is a Monorepo?

A monorepo stores multiple services and applications inside a single repository.

Example:

```txt
repo/
 ├── apps
 │   ├── api
 │   ├── admin
 │   └── gateway
 │
 ├── packages
 │   ├── shared
 │   ├── logger
 │   └── config
 │
 └── deployment
```

---

# Benefits of Monorepo

## 1. Easier Code Sharing

Shared utilities and libraries can be reused easily.

---

## 2. Unified Tooling

All services use:

- Same linting
- Same formatting
- Same CI/CD
- Same coding standards

---

## 3. Easier Refactoring

Updating shared code across services becomes simpler.

---

## 4. Better Dependency Management

All applications stay synchronized with compatible versions.

---

## 5. Better Collaboration

Developers can understand and work across the entire system.

---

## 6. Easier Integration Testing

Testing interactions between services becomes simpler.

---

# Architecture Philosophy

Sentinel focuses on:

- Clean architecture
- Scalability
- Performance
- Maintainability
- Production readiness
- Developer productivity

The goal is to provide a strong starting foundation for backend systems ranging from small projects to enterprise-scale applications.

# Project Structure

```txt
.
├── backend/
├── node_modules/
├── package/
├── package.json
├── turbo.json
├── bun.lock
└── README.md
```

---

# Folder & File Explanation

## `backend/`

This folder contains the Go backend application.

Usually includes:

- API server
- business logic
- database layer
- routes
- middleware
- services

Example:

```txt
backend/
 ├── cmd/
 ├── internal/
 ├── pkg/
 ├── configs/
 └── go.mod
```

This is the core backend part of Sentinel.

---

## `node_modules/`

This folder contains installed JavaScript/TypeScript dependencies.

Generated automatically when running:

```bash
bun install
```

Contains packages like:

- turbo
- typescript
- eslint
- prettier

---

### Why We Normally Ignore It in Git

`node_modules` can become extremely large.

Instead of uploading dependencies to GitHub, developers upload:

- `package.json`
- `bun.lock`

Other developers can reinstall dependencies using:

```bash
bun install
```

---

## `package/`

This folder usually contains shared packages/libraries inside the monorepo.

Example:

```txt
package/
 ├── logger/
 ├── config/
 ├── types/
 └── utils/
```

These shared packages can be reused across multiple applications.

---

### Why Shared Packages are Useful

They help avoid duplicate code.

Example:

instead of creating logger code in every app, create it once:

```txt
package/logger
```

and reuse everywhere.

---

## `package.json`

This is the main configuration file for JavaScript/TypeScript tooling.

It defines:

- scripts
- dependencies
- workspaces
- package manager
- project metadata

Example responsibilities:

- running development servers
- linting
- formatting
- monorepo management

---

### Example Commands

```bash
bun run dev
bun run build
bun run lint
```

All are defined inside `package.json`.

---

## `turbo.json`

Configuration file for Turborepo.

Controls:

- task execution
- caching
- dependency pipelines
- monorepo orchestration

Example:

```json
{
  "tasks": {
    "build": {
      "dependsOn": ["^build"]
    }
  }
}
```

This tells Turbo:

- build dependent packages first
- optimize execution

---

## `bun.lock`

Lock file generated by Bun.

Stores exact dependency versions.

---

### Why Lock Files are Important

Without lock files:

different developers may install different package versions.

This can create:

- inconsistent behavior
- bugs
- build failures

`bun.lock` ensures everyone uses the exact same dependency versions.

---

## `README.md`

Project documentation file.

Usually contains:

- setup instructions
- project structure
- tooling explanation
- architecture overview
- development workflow
- commands

This helps developers understand and contribute to Sentinel quickly.

---

# Why This Structure is Useful

This structure provides:

- scalability
- clean organization
- monorepo support
- reusable packages
- easier maintenance
- production-ready tooling

It is suitable for:

- backend systems
- full-stack applications
- microservices
- enterprise projects