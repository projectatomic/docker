package registry

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/docker/distribution/reference"
	registrytypes "github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/image/v1"
	"github.com/docker/docker/opts"
	flag "github.com/docker/docker/pkg/mflag"
)

// Options holds command line options.
type Options struct {
	Mirrors            opts.ListOpts
	InsecureRegistries opts.ListOpts
}

const (
	// DefaultNamespace is the default namespace
	DefaultNamespace = "docker.io"
	// DefaultRegistryVersionHeader is the name of the default HTTP header
	// that carries Registry version info
	DefaultRegistryVersionHeader = "Docker-Distribution-Api-Version"

	// IndexServer is the v1 registry server used for user auth + account creation
	IndexServer = DefaultV1Registry + "/v1/"
	// IndexName is the name of the index
	IndexName = "docker.io"

	// NotaryServer is the endpoint serving the Notary trust server
	NotaryServer = "https://notary.docker.io"

	// IndexServer = "https://registry-stage.hub.docker.com/v1/"

	// OfficialReposNamePrefix is a remote name component of official
	// repositories. It is stripped from repository local name.
	OfficialReposNamePrefix = "library/"
)

var (
	// BlockedRegistries is a set of registries that can't be contacted. A
	// special entry "*" causes all registries but those present in
	// RegistryList to be blocked.
	BlockedRegistries map[string]struct{}
	// RegistryList is a list of default registries..
	RegistryList = []string{IndexName}
	// ErrInvalidRepositoryName is an error returned if the repository name did
	// not have the correct form
	ErrInvalidRepositoryName = errors.New("Invalid repository name (ex: \"registry.domain.tld/myrepos\")")

	emptyServiceConfig = NewServiceConfig(nil)

	// V2Only controls access to legacy registries.  If it is set to true via the
	// command line flag the daemon will not attempt to contact v1 legacy registries
	V2Only = false
)

func init() {
	BlockedRegistries = make(map[string]struct{})
}

// IndexServerName returns the name of default index server.
func IndexServerName() string {
	if len(RegistryList) < 1 {
		return ""
	}
	return RegistryList[0]
}

// IndexServerAddress returns index uri of default registry.
func IndexServerAddress() string {
	if IndexServerName() == IndexName {
		return IndexServer
	} else if IndexServerName() == "" {
		return ""
	} else {
		return fmt.Sprintf("https://%s/v1/", IndexServerName())
	}
}

// InstallFlags adds command-line options to the top-level flag parser for
// the current process.
func (options *Options) InstallFlags(cmd *flag.FlagSet, usageFn func(string) string) {
	options.Mirrors = opts.NewListOpts(ValidateMirror)
	cmd.Var(&options.Mirrors, []string{"-registry-mirror"}, usageFn("Preferred Docker registry mirror"))
	options.InsecureRegistries = opts.NewListOpts(ValidateIndexName)
	cmd.Var(&options.InsecureRegistries, []string{"-insecure-registry"}, usageFn("Enable insecure registry communication"))
	cmd.BoolVar(&V2Only, []string{"-disable-legacy-registry"}, false, "Do not contact legacy registries")
}

// NewServiceConfig returns a new instance of ServiceConfig
func NewServiceConfig(options *Options) *registrytypes.ServiceConfig {
	if options == nil {
		options = &Options{
			Mirrors:            opts.NewListOpts(nil),
			InsecureRegistries: opts.NewListOpts(nil),
		}
	}

	// Localhost is by default considered as an insecure registry
	// This is a stop-gap for people who are running a private registry on localhost (especially on Boot2docker).
	//
	// TODO: should we deprecate this once it is easier for people to set up a TLS registry or change
	// daemon flags on boot2docker?
	options.InsecureRegistries.Set("127.0.0.0/8")

	config := &registrytypes.ServiceConfig{
		InsecureRegistryCIDRs: make([]*registrytypes.NetIPNet, 0),
		IndexConfigs:          make(map[string]*registrytypes.IndexInfo, 0),
		// Hack: Bypass setting the mirrors to IndexConfigs since they are going away
		// and Mirrors are only for the official registry anyways.
		Mirrors: options.Mirrors.GetAll(),
	}
	// Split --insecure-registry into CIDR and registry-specific settings.
	for _, r := range options.InsecureRegistries.GetAll() {
		// Check if CIDR was passed to --insecure-registry
		_, ipnet, err := net.ParseCIDR(r)
		if err == nil {
			// Valid CIDR.
			config.InsecureRegistryCIDRs = append(config.InsecureRegistryCIDRs, (*registrytypes.NetIPNet)(ipnet))
		} else {
			// Assume `host:port` if not CIDR.
			config.IndexConfigs[r] = &registrytypes.IndexInfo{
				Name:     r,
				Mirrors:  make([]string, 0),
				Secure:   false,
				Official: false,
			}
		}
	}

	for _, r := range RegistryList {
		var mirrors []string
		if config.IndexConfigs[r] == nil {
			// Use mirrors only with official index
			if r == IndexName {
				mirrors = config.Mirrors
			} else {
				mirrors = make([]string, 0)
			}
			config.IndexConfigs[r] = &registrytypes.IndexInfo{
				Name:     r,
				Mirrors:  mirrors,
				Secure:   isSecureIndex(config, r),
				Official: r == IndexName,
			}
		}
	}

	return config
}

// isSecureIndex returns false if the provided indexName is part of the list of insecure registries
// Insecure registries accept HTTP and/or accept HTTPS with certificates from unknown CAs.
//
// The list of insecure registries can contain an element with CIDR notation to specify a whole subnet.
// If the subnet contains one of the IPs of the registry specified by indexName, the latter is considered
// insecure.
//
// indexName should be a URL.Host (`host:port` or `host`) where the `host` part can be either a domain name
// or an IP address. If it is a domain name, then it will be resolved in order to check if the IP is contained
// in a subnet. If the resolving is not successful, isSecureIndex will only try to match hostname to any element
// of insecureRegistries.
func isSecureIndex(config *registrytypes.ServiceConfig, indexName string) bool {
	// Check for configured index, first.  This is needed in case isSecureIndex
	// is called from anything besides newIndexInfo, in order to honor per-index configurations.
	if index, ok := config.IndexConfigs[indexName]; ok {
		return index.Secure
	}
	if indexName == IndexName {
		return true
	}

	host, _, err := net.SplitHostPort(indexName)
	if err != nil {
		// assume indexName is of the form `host` without the port and go on.
		host = indexName
	}

	addrs, err := lookupIP(host)
	if err != nil {
		ip := net.ParseIP(host)
		if ip != nil {
			addrs = []net.IP{ip}
		}

		// if ip == nil, then `host` is neither an IP nor it could be looked up,
		// either because the index is unreachable, or because the index is behind an HTTP proxy.
		// So, len(addrs) == 0 and we're not aborting.
	}

	// Try CIDR notation only if addrs has any elements, i.e. if `host`'s IP could be determined.
	for _, addr := range addrs {
		for _, ipnet := range config.InsecureRegistryCIDRs {
			// check if the addr falls in the subnet
			if (*net.IPNet)(ipnet).Contains(addr) {
				return false
			}
		}
	}

	return true
}

// ValidateMirror validates an HTTP(S) registry mirror
func ValidateMirror(val string) (string, error) {
	uri, err := url.Parse(val)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid URI", val)
	}

	if uri.Scheme != "http" && uri.Scheme != "https" {
		return "", fmt.Errorf("Unsupported scheme %s", uri.Scheme)
	}

	if uri.Path != "" || uri.RawQuery != "" || uri.Fragment != "" {
		return "", fmt.Errorf("Unsupported path/query/fragment at end of the URI")
	}

	return fmt.Sprintf("%s://%s/", uri.Scheme, uri.Host), nil
}

// ValidateIndexName validates an index name.
func ValidateIndexName(val string) (string, error) {
	// 'index.docker.io' => 'docker.io'
	if val == "index."+IndexName {
		val = IndexName
	}
	for _, r := range RegistryList {
		if val == r {
			break
		}
		if val == "index."+r {
			val = r
		}
	}
	if strings.HasPrefix(val, "-") || strings.HasSuffix(val, "-") {
		return "", fmt.Errorf("Invalid index name (%s). Cannot begin or end with a hyphen.", val)
	}
	// *TODO: Check if valid hostname[:port]/ip[:port]?
	return val, nil
}

func validateRemoteName(remoteName reference.Named) error {
	remoteNameStr := remoteName.Name()
	if !strings.Contains(remoteNameStr, "/") {
		// the repository name must not be a valid image ID
		if err := v1.ValidateID(remoteNameStr); err == nil {
			return fmt.Errorf("Invalid repository name (%s), cannot specify 64-byte hexadecimal strings", remoteName)
		}
	}
	return nil
}

func validateNoSchema(reposName string) error {
	if strings.Contains(reposName, "://") {
		// It cannot contain a scheme!
		return ErrInvalidRepositoryName
	}
	return nil
}

// ValidateRepositoryName validates a repository name
func ValidateRepositoryName(reposName reference.Named) error {
	_, _, err := loadRepositoryName(reposName)
	return err
}

// loadRepositoryName returns the repo name splitted into index name
// and remote repo name. It returns an error if the name is not valid.
func loadRepositoryName(reposName reference.Named) (string, reference.Named, error) {
	if err := validateNoSchema(reposName.Name()); err != nil {
		return "", nil, err
	}
	indexName, remoteName, err := splitReposName(reposName, false)

	if indexName, err = ValidateIndexName(indexName); err != nil {
		return "", nil, err
	}
	if err = validateRemoteName(remoteName); err != nil {
		return "", nil, err
	}
	return indexName, remoteName, nil
}

// IsReferenceFullyQualified determines whether the given reposName has prepended
// name of index.
func IsReferenceFullyQualified(reposName reference.Named) bool {
	indexName, _, _ := splitReposName(reposName, false)
	return indexName != ""
}

// newIndexInfo returns IndexInfo configuration from indexName
func newIndexInfo(config *registrytypes.ServiceConfig, indexName string) (*registrytypes.IndexInfo, error) {
	var err error
	indexName, err = ValidateIndexName(indexName)
	if err != nil {
		return nil, err
	}

	// Return any configured index info, first.
	if index, ok := config.IndexConfigs[indexName]; ok {
		return index, nil
	}

	// Construct a non-configured index info.
	index := &registrytypes.IndexInfo{
		Name:     indexName,
		Mirrors:  make([]string, 0),
		Official: indexName == IndexName,
		Secure:   isSecureIndex(config, indexName),
	}

	return index, nil
}

// GetAuthConfigKey special-cases using the full index address of the official
// index as the AuthConfig key, and uses the (host)name[:port] for private indexes.
func GetAuthConfigKey(index *registrytypes.IndexInfo) string {
	if index.Official {
		return IndexServer
	}
	return index.Name
}

// splitReposName breaks a reposName into an index name and remote name
// fixMissingIndex says to return current index server name if missing in
// reposName
func splitReposName(reposName reference.Named, fixMissingIndex bool) (indexName string, remoteName reference.Named, err error) {
	var remoteNameStr string
	indexName, remoteNameStr = reference.SplitHostname(reposName)
	if indexName == "" || (!strings.Contains(indexName, ".") &&
		!strings.Contains(indexName, ":") && indexName != "localhost") {
		// This is a Docker Index repos (ex: samalba/hipache or ubuntu)
		// 'docker.io'
		if fixMissingIndex {
			indexName = IndexServerName()
		} else {
			indexName = ""
		}
		remoteName = reposName
	} else {
		remoteName, err = reference.WithName(remoteNameStr)
	}
	return
}

// IsIndexBlocked allows to check whether index/registry or endpoint
// is on a block list.
func IsIndexBlocked(indexName string) bool {
	if _, ok := BlockedRegistries[indexName]; ok {
		return true
	}
	if _, ok := BlockedRegistries["*"]; ok {
		for _, name := range RegistryList {
			if indexName == name {
				return false
			}
		}
		return true
	}
	return false
}

// newRepositoryInfo validates and breaks down a repository name into a RepositoryInfo
func newRepositoryInfo(config *registrytypes.ServiceConfig, reposName reference.Named) (*RepositoryInfo, error) {
	if err := validateNoSchema(reposName.Name()); err != nil {
		return nil, err
	}

	repoInfo := &RepositoryInfo{}
	var (
		indexName string
		err       error
	)

	indexName, repoInfo.RemoteName, err = loadRepositoryName(reposName)
	if err != nil {
		return nil, err
	}
	if indexName == "" {
		indexName = IndexServerName()
	}

	repoInfo.Index, err = newIndexInfo(config, indexName)
	if err != nil {
		return nil, err
	}

	if repoInfo.Index.Official {
		normalizedName, err := normalizeLibraryRepoName(repoInfo.RemoteName)
		if err != nil {
			return nil, err
		}
		repoInfo.RemoteName = normalizedName

		// If the normalized name does not contain a '/' (e.g. "foo")
		// then it is an official repo.
		if strings.IndexRune(repoInfo.RemoteName.Name(), '/') == -1 {
			repoInfo.Official = true
			// Fix up remote name for official repos.
			repoInfo.RemoteName, err = reference.WithName(OfficialReposNamePrefix + repoInfo.RemoteName.Name())
			if err != nil {
				return nil, err
			}
		}

		repoInfo.CanonicalName, err = reference.WithName(IndexName + "/" + repoInfo.RemoteName.Name())
		if err != nil {
			return nil, err
		}
		repoInfo.LocalName, err = reference.WithName(repoInfo.Index.Name + "/" + normalizedName.Name())
		if err != nil {
			return nil, err
		}
	} else {
		repoInfo.LocalName, err = localNameFromRemote(repoInfo.Index.Name, repoInfo.RemoteName)
		if err != nil {
			return nil, err
		}
		repoInfo.CanonicalName = repoInfo.LocalName
	}

	return repoInfo, nil
}

// ParseRepositoryInfo performs the breakdown of a repository name into a RepositoryInfo, but
// lacks registry configuration.
func ParseRepositoryInfo(reposName reference.Named) (*RepositoryInfo, error) {
	return newRepositoryInfo(emptyServiceConfig, reposName)
}

// ParseSearchIndexInfo will use repository name to get back an indexInfo.
func ParseSearchIndexInfo(reposName string) (*registrytypes.IndexInfo, error) {
	indexName, _ := splitReposSearchTerm(reposName, true)

	indexInfo, err := newIndexInfo(emptyServiceConfig, indexName)
	if err != nil {
		return nil, err
	}
	return indexInfo, nil
}

// NormalizeLocalName transforms a repository name into a normalized LocalName
// Passes through the name without transformation on error (image id, etc)
// It does not use the repository info because we don't want to load
// the repository index and do request over the network. Returned reference
// will always be fully qualified unless an error occurs or there's no
// default registry.
func NormalizeLocalName(name reference.Named, keepUnqualified bool) reference.Named {
	indexName, remoteName, err := loadRepositoryName(name)
	if err != nil {
		return name
	}

	fullyQualified := true
	if indexName == "" {
		indexName = IndexServerName()
		fullyQualified = false
	}
	fullyQualify := !keepUnqualified || fullyQualified

	var officialIndex bool
	// Return any configured index info, first.
	if index, ok := emptyServiceConfig.IndexConfigs[indexName]; ok {
		officialIndex = index.Official
	}

	if officialIndex {
		localName := remoteName
		if fullyQualify {
			localName, err = reference.WithName(IndexName + "/" + remoteName.Name())
			if err != nil {
				return name
			}
		}
		localName, err = normalizeLibraryRepoName(localName)
		if err != nil {
			return name
		}
		return localName
	}

	if !fullyQualify {
		indexName = ""
	}
	localName, err := localNameFromRemote(indexName, remoteName)
	if err != nil {
		return name
	}
	return localName
}

// normalizeLibraryRepoName removes the library prefix from
// the repository name for official repos.
func normalizeLibraryRepoName(name reference.Named) (reference.Named, error) {
	indexName, remoteName, err := splitReposName(name, false)
	if err != nil {
		return nil, err
	}
	remoteName, err = reference.WithName(strings.TrimPrefix(remoteName.Name(), OfficialReposNamePrefix))
	if err != nil {
		return nil, err
	}
	if indexName == "" {
		return remoteName, nil
	}
	return reference.WithName(indexName + "/" + remoteName.Name())
}

// localNameFromRemote combines the index name and the repo remote name
// to generate a repo local name.
func localNameFromRemote(indexName string, remoteName reference.Named) (reference.Named, error) {
	if indexName == "" {
		return remoteName, nil
	}
	return reference.WithName(indexName + "/" + remoteName.Name())
}

// NormalizeLocalReference transforms a reference to use a normalized LocalName
// for the name poriton. Passes through the reference without transformation on
// error.
func NormalizeLocalReference(ref reference.Named, keepUnqualified bool) reference.Named {
	localName := NormalizeLocalName(ref, keepUnqualified)
	if tagged, isTagged := ref.(reference.Tagged); isTagged {
		newRef, err := reference.WithTag(localName, tagged.Tag())
		if err != nil {
			return ref
		}
		return newRef
	} else if digested, isDigested := ref.(reference.Digested); isDigested {
		newRef, err := reference.WithDigest(localName, digested.Digest())
		if err != nil {
			return ref
		}
		return newRef
	}
	return localName
}

// FullyQualifyReferenceWith joins given index name with reference. If the
// reference is already qualified or resulting reference is invalid, an error
// will be returned.
func FullyQualifyReferenceWith(indexName string, ref reference.Named) (reference.Named, error) {
	if indexName == "" {
		return nil, fmt.Errorf("index name cannot be empty")
	}
	fqn, err := reference.WithName(indexName + "/" + ref.Name())
	if err != nil {
		return nil, err
	}
	fqn = NormalizeLocalName(fqn, false)
	if tagged, isTagged := ref.(reference.Tagged); isTagged {
		newRef, err := reference.WithTag(fqn, tagged.Tag())
		if err != nil {
			return nil, err
		}
		return newRef, nil
	} else if digested, isDigested := ref.(reference.Digested); isDigested {
		newRef, err := reference.WithDigest(fqn, digested.Digest())
		if err != nil {
			return nil, err
		}
		return newRef, nil
	}
	return fqn, nil
}

// SubstituteReferenceName creates a new image reference from given ref with
// its *name* part substituted for reposName.
func SubstituteReferenceName(ref reference.Named, reposName string) (newRef reference.Named, err error) {
	reposNameRef, err := reference.WithName(reposName)
	if err != nil {
		return nil, err
	}
	if tagged, isTagged := ref.(reference.Tagged); isTagged {
		newRef, err = reference.WithTag(reposNameRef, tagged.Tag())
		if err != nil {
			return nil, err
		}
	} else if digested, isDigested := ref.(reference.Digested); isDigested {
		newRef, err = reference.WithDigest(reposNameRef, digested.Digest())
		if err != nil {
			return nil, err
		}
	} else {
		newRef = reposNameRef
	}
	return
}
