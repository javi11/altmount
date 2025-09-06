# AltMount

A WebDAV server backed by NZB/Usenet that provides seamless access to Usenet content through standard WebDAV protocols.

## 📖 Documentation

**[View Full Documentation →](https://javi11.github.io/altmount/)**

Complete setup guides, configuration options, API reference, and troubleshooting information.

## Quick Start

### Docker (Recommended)

```bash
docker run -d \
  --name altmount \
  -p 8080:8080 \
  -v ./config:/app/config \
  javi11/altmount:latest
```

### CLI Installation

```bash
go install github.com/javi11/altmount@latest
altmount serve --config config.yaml
```

## Links

- 📚 [Documentation](https://altmount.kipsilabs.top)
- 🐛 [Issues](https://github.com/javi11/altmount/issues)
- 💬 [Discussions](https://github.com/javi11/altmount/discussions)

## Contributing

See the [Development Guide](https://javi11.github.io/altmount/docs/development/setup) for information on setting up a development environment and contributing to the project.

## License

This project is licensed under the terms specified in the [LICENSE](LICENSE) file.
