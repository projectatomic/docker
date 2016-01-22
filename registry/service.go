package registry

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/registry/client/auth"
	"github.com/docker/docker/reference"
	"github.com/docker/engine-api/types"
	registrytypes "github.com/docker/engine-api/types/registry"
)

const (
	// DefaultSearchLimit is the default value for maximum number of returned search results.
	DefaultSearchLimit = 25
)

// Service is the interface defining what a registry service should implement.
type Service interface {
	Auth(ctx context.Context, authConfig *types.AuthConfig, userAgent string) (status, token string, err error)
	LookupPullEndpoints(hostname string) (endpoints []APIEndpoint, err error)
	LookupPushEndpoints(hostname string) (endpoints []APIEndpoint, err error)
	ResolveRepository(name reference.Named) (*RepositoryInfo, error)
	ResolveIndex(name string) (*registrytypes.IndexInfo, error)
	Search(ctx context.Context, term string, limit int, authConfigs map[string]types.AuthConfig, userAgent string, headers map[string][]string, noIndex bool) ([]registrytypes.SearchResultExt, error)
	ServiceConfig() *registrytypes.ServiceConfig
	TLSConfig(hostname string) (*tls.Config, error)
}

// DefaultService is a registry service. It tracks configuration data such as a list
// of mirrors.
type DefaultService struct {
	config *serviceConfig
}

// NewService returns a new instance of DefaultService ready to be
// installed into an engine.
func NewService(options ServiceOptions) *DefaultService {
	return &DefaultService{
		config: newServiceConfig(options),
	}
}

// ServiceConfig returns the public registry service configuration.
func (s *DefaultService) ServiceConfig() *registrytypes.ServiceConfig {
	return &s.config.ServiceConfig
}

// Auth contacts the public registry with the provided credentials,
// and returns OK if authentication was successful.
// It can be used to verify the validity of a client's credentials.
func (s *DefaultService) Auth(ctx context.Context, authConfig *types.AuthConfig, userAgent string) (status, token string, err error) {
	// TODO Use ctx when searching for repositories
	serverAddress := authConfig.ServerAddress
	if serverAddress == "" {
		// Use the official registry address if not specified.
		serverAddress = IndexServerAddress()
	}
	if serverAddress == "" {
		return "", "", fmt.Errorf("No configured registry to authenticate to.")
	}
	if !strings.HasPrefix(serverAddress, "https://") && !strings.HasPrefix(serverAddress, "http://") {
		serverAddress = "https://" + serverAddress
	}
	u, err := url.Parse(serverAddress)
	if err != nil {
		return "", "", fmt.Errorf("unable to parse server address: %v", err)
	}

	endpoints, err := s.LookupPushEndpoints(u.Host)
	if err != nil {
		return "", "", err
	}

	for _, endpoint := range endpoints {
		login := loginV2
		if endpoint.Version == APIVersion1 {
			login = loginV1
		}

		status, token, err = login(authConfig, endpoint, userAgent)
		if err == nil {
			return
		}
		if fErr, ok := err.(fallbackError); ok {
			err = fErr.err
			logrus.Infof("Error logging in to %s endpoint, trying next endpoint: %v", endpoint.Version, err)
			continue
		}
		return "", "", err
	}

	return "", "", err
}

type by func(fst, snd *registrytypes.SearchResultExt) bool

type searchResultSorter struct {
	Results []registrytypes.SearchResultExt
	By      func(fst, snd *registrytypes.SearchResultExt) bool
}

func (by by) Sort(results []registrytypes.SearchResultExt) {
	rs := &searchResultSorter{
		Results: results,
		By:      by,
	}
	sort.Sort(rs)
}

func (s *searchResultSorter) Len() int {
	return len(s.Results)
}

func (s *searchResultSorter) Swap(i, j int) {
	s.Results[i], s.Results[j] = s.Results[j], s.Results[i]
}

func (s *searchResultSorter) Less(i, j int) bool {
	return s.By(&s.Results[i], &s.Results[j])
}

// Factory for search result comparison function. Either it takes index name
// into consideration or not.
func getSearchResultsCmpFunc(withIndex bool) by {
	// Compare two items in the result table of search command. First compare
	// the index we found the result in. Second compare their rating. Then
	// compare their fully qualified name (registry/name).
	less := func(fst, snd *registrytypes.SearchResultExt) bool {
		if withIndex {
			if fst.IndexName != snd.IndexName {
				return fst.IndexName < snd.IndexName
			}
			if fst.StarCount != snd.StarCount {
				return fst.StarCount > snd.StarCount
			}
		}
		if fst.RegistryName != snd.RegistryName {
			return fst.RegistryName < snd.RegistryName
		}
		if !withIndex {
			if fst.StarCount != snd.StarCount {
				return fst.StarCount > snd.StarCount
			}
		}
		if fst.Name != snd.Name {
			return fst.Name < snd.Name
		}
		return fst.Description < snd.Description
	}
	return less
}

func (s *DefaultService) searchTerm(term string, limit int, authConfigs map[string]types.AuthConfig, userAgent string, headers map[string][]string, noIndex bool, outs *[]registrytypes.SearchResultExt) error {
	if err := validateNoScheme(term); err != nil {
		return err
	}

	indexName, remoteName := splitReposSearchTerm(term, true)

	index, err := newIndexInfo(s.config, indexName)
	if err != nil {
		return err
	}

	// *TODO: Search multiple indexes.
	endpoint, err := NewV1Endpoint(index, userAgent, http.Header(headers))
	if err != nil {
		return err
	}

	ac := ResolveAuthConfig(authConfigs, index)
	authConfig := &ac
	var client *http.Client
	if authConfig != nil && authConfig.IdentityToken != "" && authConfig.Username != "" {
		creds := NewStaticCredentialStore(authConfig)
		scopes := []auth.Scope{
			auth.RegistryScope{
				Name:    "catalog",
				Actions: []string{"search"},
			},
		}

		modifiers := DockerHeaders(userAgent, nil)
		v2Client, foundV2, err := v2AuthHTTPClient(endpoint.URL, endpoint.client.Transport, modifiers, creds, scopes)
		if err != nil {
			if fErr, ok := err.(fallbackError); ok {
				logrus.Errorf("Cannot use identity token for search, v2 auth not supported: %v", fErr.err)
			} else {
				return err
			}
		} else if foundV2 {
			// Copy non transport http client features
			v2Client.Timeout = endpoint.client.Timeout
			v2Client.CheckRedirect = endpoint.client.CheckRedirect
			v2Client.Jar = endpoint.client.Jar

			logrus.Debugf("using v2 client for search to %s", endpoint.URL)
			client = v2Client
		}
	}

	if client == nil {
		client = endpoint.client
		if err := authorizeClient(client, authConfig, endpoint); err != nil {
			return err
		}
	}

	r, err := NewSession(client, authConfig, endpoint)
	if err != nil {
		return err
	}

	var results *registrytypes.SearchResults
	if index.Official {
		localName := remoteName
		if strings.HasPrefix(localName, reference.DefaultRepoPrefix) {
			// If pull "library/foo", it's stored locally under "foo"
			localName = strings.SplitN(localName, "/", 2)[1]
		}

		results, err = r.SearchRepositories(localName, limit)
	} else {
		results, err = r.SearchRepositories(remoteName, limit)
	}
	if err != nil || results.NumResults < 1 {
		return err
	}

	newOuts := make([]registrytypes.SearchResultExt, len(*outs)+len(results.Results))
	for i := range *outs {
		newOuts[i] = (*outs)[i]
	}
	for i, result := range results.Results {
		item := registrytypes.SearchResultExt{
			IndexName:    index.Name,
			RegistryName: index.Name,
			StarCount:    result.StarCount,
			Name:         result.Name,
			IsOfficial:   result.IsOfficial,
			IsAutomated:  result.IsAutomated,
			Description:  result.Description,
		}
		// Check if search result is fully qualified with registry
		// If not, assume REGISTRY = INDEX
		newRegistryName, newName := splitReposSearchTerm(result.Name, false)
		if newRegistryName != "" {
			item.RegistryName, item.Name = newRegistryName, newName
		}
		newOuts[len(*outs)+i] = item
	}
	*outs = newOuts
	return nil
}

// Duplicate entries may occur in result table when omitting index from output because
// different indexes may refer to same registries.
func removeSearchDuplicates(data []registrytypes.SearchResultExt) []registrytypes.SearchResultExt {
	var (
		prevIndex = 0
		res       []registrytypes.SearchResultExt
	)

	if len(data) > 0 {
		res = []registrytypes.SearchResultExt{data[0]}
	}
	for i := 1; i < len(data); i++ {
		prev := res[prevIndex]
		curr := data[i]
		if prev.RegistryName == curr.RegistryName && prev.Name == curr.Name {
			// Repositories are equal, delete one of them.
			// Find out whose index has higher priority (the lower the number
			// the higher the priority).
			var prioPrev, prioCurr int
			for prioPrev = 0; prioPrev < len(DefaultRegistries); prioPrev++ {
				if prev.IndexName == DefaultRegistries[prioPrev] {
					break
				}
			}
			for prioCurr = 0; prioCurr < len(DefaultRegistries); prioCurr++ {
				if curr.IndexName == DefaultRegistries[prioCurr] {
					break
				}
			}
			if prioPrev > prioCurr || (prioPrev == prioCurr && prev.StarCount < curr.StarCount) {
				// replace previous entry with current one
				res[prevIndex] = curr
			} // otherwise keep previous entry
		} else {
			prevIndex++
			res = append(res, curr)
		}
	}
	return res
}

// Search queries several registries for images matching the specified
// search terms, and returns the results.
func (s *DefaultService) Search(ctx context.Context, term string, limit int, authConfigs map[string]types.AuthConfig, userAgent string, headers map[string][]string, noIndex bool) ([]registrytypes.SearchResultExt, error) {
	results := []registrytypes.SearchResultExt{}
	cmpFunc := getSearchResultsCmpFunc(!noIndex)

	// helper for concurrent queries
	searchRoutine := func(term string, c chan<- error) {
		err := s.searchTerm(term, limit, authConfigs, userAgent, headers, noIndex, &results)
		c <- err
	}

	if isReposSearchTermFullyQualified(term) {
		if err := s.searchTerm(term, limit, authConfigs, userAgent, headers, noIndex, &results); err != nil {
			return nil, err
		}
	} else if len(DefaultRegistries) < 1 {
		return nil, fmt.Errorf("No configured repository to search.")
	} else {
		var (
			err              error
			successfulSearch = false
			resultChan       = make(chan error)
		)
		// query all registries in parallel
		for i, r := range DefaultRegistries {
			tmp := term
			if i > 0 {
				tmp = fmt.Sprintf("%s/%s", r, term)
			}
			go searchRoutine(tmp, resultChan)
		}
		for range DefaultRegistries {
			err = <-resultChan
			if err == nil {
				successfulSearch = true
			} else {
				logrus.Errorf("%s", err.Error())
			}
		}
		if !successfulSearch {
			return nil, err
		}
	}
	by(cmpFunc).Sort(results)
	if noIndex {
		results = removeSearchDuplicates(results)
	}
	return results, nil
}

// splitReposSearchTerm breaks a search term into an index name and remote name
func splitReposSearchTerm(reposName string, fixMissingIndex bool) (string, string) {
	nameParts := strings.SplitN(reposName, "/", 2)
	var indexName, remoteName string
	if len(nameParts) == 1 || (!strings.Contains(nameParts[0], ".") &&
		!strings.Contains(nameParts[0], ":") && nameParts[0] != "localhost") {
		// This is a Docker Index repos (ex: samalba/hipache or ubuntu)
		// 'docker.io'
		if fixMissingIndex {
			indexName = IndexServerName()
		} else {
			indexName = ""
		}
		remoteName = reposName
	} else {
		indexName = nameParts[0]
		remoteName = nameParts[1]
	}
	return indexName, remoteName
}

func isReposSearchTermFullyQualified(term string) bool {
	indexName, _ := splitReposSearchTerm(term, false)
	return indexName != ""
}

// ResolveRepository splits a repository name into its components
// and configuration of the associated registry.
func (s *DefaultService) ResolveRepository(name reference.Named) (*RepositoryInfo, error) {
	return newRepositoryInfo(s.config, name)
}

// ResolveIndex takes indexName and returns index info
func (s *DefaultService) ResolveIndex(name string) (*registrytypes.IndexInfo, error) {
	return newIndexInfo(s.config, name)
}

// APIEndpoint represents a remote API endpoint
type APIEndpoint struct {
	Mirror       bool
	URL          *url.URL
	Version      APIVersion
	Official     bool
	TrimHostname bool
	TLSConfig    *tls.Config
}

// ToV1Endpoint returns a V1 API endpoint based on the APIEndpoint
func (e APIEndpoint) ToV1Endpoint(userAgent string, metaHeaders http.Header) (*V1Endpoint, error) {
	return newV1Endpoint(*e.URL, e.TLSConfig, userAgent, metaHeaders)
}

// TLSConfig constructs a client TLS configuration based on server defaults
func (s *DefaultService) TLSConfig(hostname string) (*tls.Config, error) {
	return newTLSConfig(hostname, isSecureIndex(s.config, hostname))
}

func (s *DefaultService) tlsConfigForMirror(mirrorURL *url.URL) (*tls.Config, error) {
	return s.TLSConfig(mirrorURL.Host)
}

// LookupPullEndpoints creates a list of endpoints to try to pull from, in order of preference.
// It gives preference to v2 endpoints over v1, mirrors over the actual
// registry, and HTTPS over plain HTTP.
func (s *DefaultService) LookupPullEndpoints(hostname string) (endpoints []APIEndpoint, err error) {
	return s.lookupEndpoints(hostname)
}

// LookupPushEndpoints creates a list of endpoints to try to push to, in order of preference.
// It gives preference to v2 endpoints over v1, and HTTPS over plain HTTP.
// Mirrors are not included.
func (s *DefaultService) LookupPushEndpoints(hostname string) (endpoints []APIEndpoint, err error) {
	allEndpoints, err := s.lookupEndpoints(hostname)
	if err == nil {
		for _, endpoint := range allEndpoints {
			if !endpoint.Mirror {
				endpoints = append(endpoints, endpoint)
			}
		}
	}
	return endpoints, err
}

func (s *DefaultService) lookupEndpoints(hostname string) (endpoints []APIEndpoint, err error) {
	endpoints, err = s.lookupV2Endpoints(hostname)
	if err != nil {
		return nil, err
	}

	if s.config.V2Only {
		return endpoints, nil
	}
	legacyEndpoints, err := s.lookupV1Endpoints(hostname)
	if err != nil {
		return nil, err
	}
	endpoints = append(endpoints, legacyEndpoints...)

	filtered := filterBlockedEndpoints(endpoints)
	if len(filtered) == 0 && len(endpoints) > 0 {
		return nil, fmt.Errorf("All endpoints blocked.")
	}

	return filtered, nil
}
