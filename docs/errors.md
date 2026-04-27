# Errors

Tool execution errors are returned as MCP tool results with `isError: true`.

The text content and structured content both use this shape:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "human-readable message",
    "detail": {}
  }
}
```

Known error codes:

- `invalid_request`
- `not_found`
- `permission_denied`
- `unavailable`
- `unsupported`
- `mutation_disabled`
- `jenkins_error`
