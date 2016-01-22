package reference

import (
	"fmt"
	"strings"

	"github.com/docker/distribution/reference"
)

const (
	DefaultRepoPrefix = "library/"
)

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

func RemoteName(ref reference.Named) string {
	h, p := reference.SplitHostname(ref)
	if h == "docker.io" && !strings.ContainsRune(p, '/') {
		p = "library/" + p
	}
	return p
}

// UnqualifyReference ...
func UnqualifyReference(ref reference.Named) reference.Named {
	_, remoteName, err := SplitReposName(ref)
	if err != nil {
		return ref
	}
	newRef, err := SubstituteReferenceName(ref, remoteName.Name())
	if err != nil {
		return ref
	}
	return newRef
}

// QualifyUnqualifiedReference ...
func QualifyUnqualifiedReference(ref reference.Named, indexName string) (reference.Named, error) {
	if !isValidHostname(indexName) {
		return nil, fmt.Errorf("Invalid hostname %q", indexName)
	}
	orig, remoteName, err := SplitReposName(ref)
	if err != nil {
		return nil, err
	}
	if orig == "" {
		return SubstituteReferenceName(ref, indexName+"/"+remoteName.Name())
	}
	return ref, nil
}

// IsReferenceFullyQualified determines whether the given reposName has prepended
// name of index.
func IsReferenceFullyQualified(reposName reference.Named) bool {
	indexName, _, _ := SplitReposName(reposName)
	return indexName != ""
}

// SplitReposName breaks a reposName into an index name and remote name
func SplitReposName(reposName reference.Named) (indexName string, remoteName reference.Named, err error) {
	var remoteNameStr string
	indexName, remoteNameStr = reference.SplitHostname(reposName)
	if !isValidHostname(indexName) {
		// This is a Docker Index repos (ex: samalba/hipache or ubuntu)
		// 'docker.io'
		indexName = ""
		remoteName = reposName
	} else {
		remoteName, err = reference.WithName(remoteNameStr)
	}
	return
}

func isValidHostname(hostname string) bool {
	return hostname != "" && !strings.Contains(hostname, "/") &&
		(strings.Contains(hostname, ".") ||
			strings.Contains(hostname, ":") || hostname == "localhost")
}
