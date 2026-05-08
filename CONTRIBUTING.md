# Contributing to tap-sdk

## Getting Started

1. Fork the repository
2. Clone your fork
3. Create a feature branch from `main`
4. Make your changes
5. Submit a pull request

## Development Setup

### Prerequisites

- Go 1.25.7+
- Docker (for linting)
- A running `tapd` instance (for integration tests)

### Building

```bash
make build
```

### Running Tests

```bash
# All unit tests
make unit

# Specific package
make unit pkg=.

# Specific test case
make unit pkg=. case=TestParseAssetID
```

### Linting

```bash
make lint
```

### Formatting

```bash
make fmt
```

## Code Style

This project follows Lightning Labs coding conventions. The full style guide
is in `.gemini/styleguide.md`. Key points:

### Line Length

Lines MUST NOT exceed 80 characters. Tabs count as 8 spaces.

### Function Comments

Every exported function must have a comment that starts with the function
name and forms a complete sentence.

```go
// DeriveScriptKey derives a new script key for receiving assets.
// The script key includes both the internal key and the tweaked
// Taproot output key.
func (w *Wallet) DeriveScriptKey(ctx context.Context) (
	*ScriptKey, error) {
```

### Wrapping

If a function call exceeds 80 characters, place the closing parenthesis on
its own line:

```go
value, err := bar(
	a, b, c,
)
```

### Error Messages

Keep error and log messages compact:

```go
return fmt.Errorf("failed to fund transfer with %d "+
	"recipients", len(recipients))
```

### Tests

Use table-driven tests with `require` assertions:

```go
func TestParseAssetID(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    AssetID
		wantErr bool
	}{
		{
			name:  "valid 32 bytes",
			input: bytes.Repeat([]byte{0xab}, 32),
			want:  func() AssetID { /* ... */ }(),
		},
		{
			name:    "too short",
			input:   []byte{0x01},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAssetID(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
```

## Commit Messages

Format: `subsystem: short description`

- `subsystem` is the package primarily affected
- Use `+` or `,` for multiple packages: `wallet+grpc: add balance types`
- Use `multi:` for widespread changes
- Keep subject under 50 characters
- Use present tense ("add feature", not "added feature")
- Body explains "what" and "why", wrapped at 72 characters

## Pull Requests

### Before Submitting

- [ ] All tests pass (`make unit`)
- [ ] Linting passes (`make lint`)
- [ ] Code is formatted (`make fmt`)
- [ ] Commits are granular and logical (not incremental)
- [ ] New features have tests
- [ ] New features have documentation
- [ ] CHANGELOG.md is updated

### Design Documents

For non-trivial changes (new RPC wrappers, architecture changes, new
packages), create a design document under `docs/design/` before
implementing. This ensures alignment before code is written.

## Architecture

See [AGENTS.md](AGENTS.md) for architecture overview and
[docs/architecture.md](docs/architecture.md) for detailed design.
