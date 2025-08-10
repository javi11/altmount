package utils

import (
	"net/url"
	"strconv"
	"strings"
)

type contextKey string

const separator = "?ARGS?"

func (c contextKey) String() string {
	return "webdav context key " + string(c)
}

const ContentLengthKey = contextKey("contentLength")
const RangeKey = contextKey("rangeKey")
const IsCopy = contextKey("isCopy")
const Origin = contextKey("origin")

type PathWithArgs struct {
	Path string
	args url.Values
}

func (p PathWithArgs) String() string {
	args := p.args.Encode()
	if args != "" {
		return p.Path + separator + args
	}

	return p.Path
}

func (p PathWithArgs) Range() (*RangeHeader, error) {
	if p.args.Get(RangeKey.String()) == "" {
		return nil, nil
	}

	r, e := ParseRangeHeader(p.args.Get(RangeKey.String()))
	if e != nil {
		return nil, e
	}

	return r, nil
}

func (p PathWithArgs) FileSize() (int64, error) {
	return strconv.ParseInt(p.args.Get(ContentLengthKey.String()), 10, 64)
}

func (p PathWithArgs) IsCopy() bool {
	return p.args.Get(IsCopy.String()) == "true"
}

func (p PathWithArgs) Origin() string {
	return p.args.Get(Origin.String())
}

func (p PathWithArgs) SetOrigin(origin string) {
	p.args.Set(Origin.String(), origin)
}

func (p PathWithArgs) SetRange(r string) {
	p.args.Set(RangeKey.String(), r)
}

func (p PathWithArgs) SetFileSize(s string) {
	p.args.Set(ContentLengthKey.String(), s)
}

func (p PathWithArgs) SetIsCopy() {
	p.args.Set(IsCopy.String(), "true")
}

func NewPathWithArgsFromString(s string) (PathWithArgs, error) {
	p := strings.Split(s, separator)

	if len(p) > 1 {
		q, err := url.ParseQuery(p[1])
		if err != nil {
			return PathWithArgs{}, err
		}

		return PathWithArgs{
			Path: p[0],
			args: q,
		}, nil
	}

	return PathWithArgs{
		Path: p[0],
		args: url.Values{},
	}, nil
}

func NewPathWithArgs(path string) PathWithArgs {
	return PathWithArgs{
		Path: path,
		args: url.Values{},
	}
}
