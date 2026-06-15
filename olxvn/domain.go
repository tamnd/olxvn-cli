package olxvn

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the OLX Vietnam kit driver.
type Domain struct{}

func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "olxvn",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "olxvn",
			Short:  "A command line for OLX Vietnam.",
			Long: `A command line for OLX Vietnam (olx.vn).

Fetches classified listing details and category listings
from one of Vietnam's top classifieds platforms.
No API key required.`,
			Site: "https://" + Host,
			Repo: "https://github.com/tamnd/olxvn-cli",
		},
	}
}

func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "listing", Group: "classifieds", Single: true,
		URIType: "listing", Resolver: true, Summary: "Fetch a listing by slug or URL",
		Args: []kit.Arg{{Name: "ref", Help: "listing slug or URL"}}}, getListing)

	kit.Handle(app, kit.OpMeta{Name: "listings", Group: "classifieds", List: true,
		URIType: "listing", Summary: "List listings from a category",
		Args: []kit.Arg{{Name: "category", Help: "category slug (e.g. xe-may-xe-dap)"}}}, listListings)
}

func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClientWithConfig(DefaultConfig())
	if cfg.UserAgent != "" {
		c.cfg.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.cfg.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.cfg.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.cfg.Timeout = cfg.Timeout
		c.http.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type listingRef struct {
	Ref    string  `kit:"arg" help:"listing slug or URL"`
	Client *Client `kit:"inject"`
}

type listingsIn struct {
	Category string  `kit:"arg" help:"category slug (e.g. xe-may-xe-dap)"`
	Limit    int     `kit:"flag,inherit" help:"max results"`
	Client   *Client `kit:"inject"`
}

// --- handlers ---

func getListing(ctx context.Context, in listingRef, emit func(*Listing) error) error {
	slug := listingSlug(in.Ref)
	if slug == "" {
		return errs.Usage("unrecognized OLX VN listing reference: %q", in.Ref)
	}
	l, err := in.Client.GetListing(ctx, slug)
	if err != nil {
		return err
	}
	return emit(l)
}

func listListings(ctx context.Context, in listingsIn, emit func(*Listing) error) error {
	listings, err := in.Client.ListListings(ctx, in.Category, 1, in.Limit)
	if err != nil {
		return err
	}
	for _, l := range listings {
		if err := emit(l); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver ---

func (Domain) Classify(input string) (uriType, id string, err error) {
	slug := listingSlug(input)
	if slug != "" {
		return "listing", slug, nil
	}
	return "", "", errs.Usage("unrecognized OLX VN reference: %q", input)
}

func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "listing":
		return baseURL + "/item/" + strings.Trim(id, "/") + "/", nil
	default:
		return "", errs.Usage("olxvn has no resource type %q", uriType)
	}
}

// listingSlug normalises user input to a canonical slug.
func listingSlug(input string) string {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "olx.vn") || strings.HasPrefix(input, "http") {
		return extractSlug(input)
	}
	// Bare slug (may or may not contain /item/ prefix).
	slug := strings.TrimPrefix(strings.Trim(input, "/"), "item/")
	if slug != "" && !strings.Contains(slug, "/") && !strings.Contains(slug, " ") {
		return slug
	}
	return ""
}
