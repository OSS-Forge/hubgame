# Controller Progress

## 2026-03-04
- Added JWT issue/parse flow and auth middleware.
- Added storage tenant controller hooks to block cross-tenant operations.
- Added RBAC action matrix enforcement at gateway layer.
- Added dedicated controller service endpoints for token issue/verify.
- Added gateway auth flow that verifies tokens through controller service.
