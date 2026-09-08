package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"nautilus/internal/database/sessions"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/scan"
)

type options struct {
	dir, url, orgID string
	limit           int
	dryRun          bool
}

type document struct {
	name  string
	pages []string
}

func Run(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, args, os.Getenv("NAUTILUS_ADMIN_SESSION"), os.Stdout); err != nil {
		log.InferLogger("seed").Fatal("document seed failed", "error", err)
	}
}

func execute(ctx context.Context, args []string, token string, out io.Writer) error {
	opts := options{}
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&opts.dir, "dir", "data/filled/image", "directory containing JPEG/PNG scans")
	flags.StringVar(&opts.url, "url", "http://localhost:8080/api", "app API base URL")
	flags.StringVar(&opts.orgID, "org-id", "", "recipient organization UUID (required for uploads)")
	flags.IntVar(&opts.limit, "limit", 0, "maximum documents to upload; zero selects all")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "validate and list documents without uploading")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return errors.New("invalid seed flags; use seed --help")
	}
	if flags.NArg() != 0 || opts.limit < 0 {
		return errors.New("seed accepts flags only and requires a nonnegative limit")
	}
	if !opts.dryRun {
		id, err := uuid.Parse(opts.orgID)
		if err != nil {
			return errors.New("--org-id must be a recipient organization UUID")
		}
		opts.orgID = id.String()
		cookie := sessions.CreateCookie(token)
		if token == "" || cookie.Valid() != nil {
			return errors.New("set NAUTILUS_ADMIN_SESSION to an authenticated administrator session token")
		}
	}
	base, err := url.Parse(opts.url)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("--url must be an app API base URL without credentials, query, or fragment")
	}
	ip := net.ParseIP(base.Hostname())
	local := base.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if base.Scheme != "https" && (base.Scheme != "http" || !local) {
		return errors.New("--url requires HTTPS except for localhost")
	}
	docs, err := discover(opts.dir)
	if err != nil {
		return err
	}
	if opts.limit > 0 {
		docs = docs[:min(opts.limit, len(docs))]
	}
	// Validate the complete selected batch before the first upload.
	for _, doc := range docs {
		body, _, err := encode(ctx, doc)
		clear(body)
		if err != nil {
			return err
		}
		if opts.dryRun {
			fmt.Fprintf(out, "%s.pdf\t%d pages\n", doc.name, len(doc.pages))
		}
	}
	if opts.dryRun {
		fmt.Fprintf(out, "Validated %d documents; no uploads made.\n", len(docs))
		return nil
	}
	client := &http.Client{
		Timeout:       5 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	endpoint := strings.TrimRight(base.String(), "/") + "/admin/organizations/" + opts.orgID + "/documents"
	for i, doc := range docs {
		id, err := upload(ctx, client, endpoint, token, doc)
		if err != nil {
			fmt.Fprintf(out, "Stopped after %d accepted documents. The current upload may have been accepted; check before rerunning.\n", i)
			return err
		}
		fmt.Fprintf(out, "%d/%d\t%s.pdf\t%s\n", i+1, len(docs), doc.name, id)
	}
	fmt.Fprintf(out, "Accepted %d documents for processing. PDF creation and indexing continue in the worker.\n", len(docs))
	return nil
}

var pageSuffix = regexp.MustCompile(`^(.+)-page-([0-9]+)$`)

func discover(dir string) ([]document, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrap(err, "read seed directory")
	}
	groups := map[string]map[int]string{}
	for _, entry := range entries {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, errors.New("seed images must be regular files")
		}
		if !utf8.ValidString(entry.Name()) || utf8.RuneCountInString(entry.Name()) > 255 ||
			strings.ContainsFunc(entry.Name(), unicode.IsControl) || strings.Contains(entry.Name(), "\\") ||
			strings.TrimSpace(entry.Name()) != entry.Name() {
			return nil, errors.New("seed image filenames must be valid basenames of at most 255 characters without control characters")
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		page := 0
		if match := pageSuffix.FindStringSubmatch(name); match != nil {
			name = match[1]
			page, err = strconv.Atoi(match[2])
			if err != nil || page < 1 || page > scan.MaxPages {
				return nil, errors.New("seed page numbers must be between 1 and 100")
			}
		}
		if groups[name] == nil {
			groups[name] = map[int]string{}
		}
		if _, ok := groups[name][page]; ok {
			return nil, errors.New("seed directory contains duplicate page renditions")
		}
		groups[name][page] = filepath.Join(dir, entry.Name())
	}
	docs := make([]document, 0, len(groups))
	for name, pages := range groups {
		doc := document{name: name}
		if len(pages) == 1 && pages[0] != "" {
			doc.pages = []string{pages[0]}
		} else {
			// Numbered renditions supersede the standalone image of the same document.
			delete(pages, 0)
			for i := 1; i <= len(pages); i++ {
				if pages[i] == "" {
					return nil, errors.New("seed document has missing page numbers")
				}
				doc.pages = append(doc.pages, pages[i])
			}
		}
		docs = append(docs, doc)
	}
	slices.SortFunc(docs, func(a, b document) int { return strings.Compare(a.name, b.name) })
	if len(docs) == 0 {
		return nil, errors.New("seed directory contains no JPEG or PNG images")
	}
	return docs, nil
}

func encode(ctx context.Context, doc document) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	total := 0
	for i, filename := range doc.pages {
		if err := ctx.Err(); err != nil {
			clear(body.Bytes())
			return nil, "", errors.WithStack(err)
		}
		data, err := readPage(filename, scan.MaxPDFBytes-total)
		if err != nil {
			clear(body.Bytes())
			return nil, "", err
		}
		total += len(data)
		name := filepath.Base(filename)
		if i == 0 {
			name = doc.name + filepath.Ext(filename)
		}
		part, err := writer.CreateFormFile("file", name)
		if err == nil {
			_, err = part.Write(data)
		}
		clear(data)
		if err != nil {
			clear(body.Bytes())
			return nil, "", errors.Wrap(err, "encode seed multipart upload")
		}
	}
	if err := writer.Close(); err != nil {
		clear(body.Bytes())
		return nil, "", errors.Wrap(err, "finish seed multipart upload")
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func readPage(filename string, limit int) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrap(err, "open seed page")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err == nil && len(data) > limit {
		err = scan.ErrTooLarge
	}
	if err == nil {
		_, err = scan.Validate(data)
	}
	if err != nil {
		clear(data)
		return nil, errors.Wrap(err, "validate seed page")
	}
	return data, nil
}

func upload(ctx context.Context, client *http.Client, endpoint, token string, doc document) (string, error) {
	body, contentType, err := encode(ctx, doc)
	if err != nil {
		return "", err
	}
	defer clear(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("unable to create seed upload request")
	}
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(sessions.CreateCookie(token))
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("seed upload response could not be confirmed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		return "", errors.Errorf("seed upload returned HTTP %d", res.StatusCode)
	}
	var result struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil {
		return "", errors.New("seed upload returned an invalid document response")
	}
	id, err := uuid.Parse(result.Document.ID)
	if err != nil {
		return "", errors.New("seed upload returned an invalid document ID")
	}
	return id.String(), nil
}
