# Claude Instructions

## On Startup
Always read docs/PROJECT_NOTES.md first to understand the project context and current state.

## Project Commands
- Lint: `make lint`
- Test: `make test`
- Build: `make build`
- Goimports: `find ./ -type f -iname "*.go" -exec goimports -w {} \;`

## Important Files
- docs/PROJECT_NOTES.md - Contains project overview, goals, and current implementation status.
- README.md - Contains basic project information.
- Makefile - Contains build, test, and lint commands.
