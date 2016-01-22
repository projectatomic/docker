<!--[metadata]>
+++
title = "search"
description = "The search command description and usage"
keywords = ["search, hub, images"]
[menu.main]
parent = "smn_cli"
+++
<![end-metadata]-->

# search

```markdown
Usage:  docker search [OPTIONS] TERM

Search the Docker Hub for images

Options:
  -f, --filter value   Filter output based on conditions provided (default [])
                       - is-automated=(true|false)
                       - is-official=(true|false)
                       - stars=<number> - image has at least 'number' stars
      --help           Print usage
      --limit int      Max number of search results (default 25)
      --no-trunc       Don't truncate output
      --no-index       Omit index column from output
```

Search [Docker Hub](https://hub.docker.com) for images

See [*Find Public Images on Docker Hub*](../../tutorials/dockerrepos.md#searching-for-images) for
more details on finding shared images from the command line.

> **Note:**
> Search queries will only return up to 25 results

## Examples

### Search images by name

This example displays images with a name containing 'busybox':

    $ docker search busybox
    INDEX       NAME                                       DESCRIPTION                                     STARS     OFFICIAL   AUTOMATED
    docker.io   docker.io/busybox                          Busybox base image.                             436       [OK]
    docker.io   docker.io/progrium/busybox                                                                 53                   [OK]
    docker.io   docker.io/radial/busyboxplus               Full-chain, Internet enabled, busybox made...   8                    [OK]
    docker.io   docker.io/odise/busybox-python                                                             3                    [OK]
    docker.io   docker.io/azukiapp/busybox                 This image is meant to be used as the base...   2                    [OK]
    docker.io   docker.io/multiarch/busybox                multiarch ports of ubuntu-debootstrap           2                    [OK]
    docker.io   docker.io/elektritter/busybox-teamspeak    Leightweight teamspeak3 container based on...   1                    [OK]
    docker.io   docker.io/odise/busybox-curl                                                               1                    [OK]
    docker.io   docker.io/ofayau/busybox-jvm               Prepare busybox to install a 32 bits JVM.       1                    [OK]
    docker.io   docker.io/ofayau/busybox-libc32            Busybox with 32 bits (and 64 bits) libs         1                    [OK]
    docker.io   docker.io/peelsky/zulu-openjdk-busybox                                                     1                    [OK]
    docker.io   docker.io/sequenceiq/busybox                                                               1                    [OK]
    docker.io   docker.io/shingonoide/archlinux-busybox    Arch Linux, a lightweight and flexible Lin...   1                    [OK]
    docker.io   docker.io/skomma/busybox-data              Docker image suitable for data volume cont...   1                    [OK]
    docker.io   docker.io/socketplane/busybox                                                              1                    [OK]
    docker.io   docker.io/buddho/busybox-java8             Java8 on Busybox                                0                    [OK]
    docker.io   docker.io/container4armhf/armhf-busybox    Automated build of Busybox for armhf devic...   0                    [OK]
    docker.io   docker.io/ggtools/busybox-ubuntu           Busybox ubuntu version with extra goodies       0                    [OK]
    docker.io   docker.io/nikfoundas/busybox-confd         Minimal busybox based distribution of confd     0                    [OK]
    docker.io   docker.io/openshift/busybox-http-app                                                       0                    [OK]
    docker.io   docker.io/oveits/docker-nginx-busybox      This is a tiny NginX docker image based on...   0                    [OK]
    docker.io   docker.io/powellquiring/busybox                                                            0                    [OK]
    docker.io   docker.io/simplexsys/busybox-cli-powered   Docker busybox images, with a few often us...   0                    [OK]
    docker.io   docker.io/stolus/busybox                                                                   0                    [OK]
    docker.io   docker.io/williamyeh/busybox-sh            Docker image for BusyBox's sh                   0                    [OK]

### Display non-truncated description (--no-trunc)

This example displays images with a name containing 'busybox',
at least 3 stars and the description isn't truncated in the output:

    $ docker search --stars=3 --no-trunc busybox
    NAME                 DESCRIPTION                                                                               STARS     OFFICIAL   AUTOMATED
    busybox              Busybox base image.                                                                       325       [OK]       
    progrium/busybox                                                                                               50                   [OK]
    radial/busyboxplus   Full-chain, Internet enabled, busybox made from scratch. Comes in git and cURL flavors.   8                    [OK]

## Limit search results (--limit)

The flag `--limit` is the maximium number of results returned by a search. This value could
be in the range between 1 and 100. The default value of `--limit` is 25.


## Filtering

The filtering flag (`-f` or `--filter`) format is a `key=value` pair. If there is more
than one filter, then pass multiple flags (e.g. `--filter "foo=bar" --filter "bif=baz"`)

The currently supported filters are:

* stars (int - number of stars the image has)
* is-automated (true|false) - is the image automated or not
* is-official (true|false) - is the image official or not


### stars

This example displays images with a name containing 'busybox' and at
least 3 stars:

    $ docker search --filter stars=3 busybox
    INDEX       NAME                             DESCRIPTION                                     STARS     OFFICIAL   AUTOMATED
    docker.io   docker.io/busybox                Busybox base image.                             436       [OK]
    docker.io   docker.io/progrium/busybox                                                       53                   [OK]
    docker.io   docker.io/radial/busyboxplus     Full-chain, Internet enabled, busybox made...   8                    [OK]
    docker.io   docker.io/odise/busybox-python                                                   3                    [OK]

### is-automated

This example displays images with a name containing 'busybox'
and are automated builds:

    $ docker search --filter "is-automated" busybox
    INDEX       NAME                             DESCRIPTION                                     STARS     OFFICIAL   AUTOMATED
    docker.io   docker.io/progrium/busybox                                                       53                   [OK]
    docker.io   docker.io/radial/busyboxplus     Full-chain, Internet enabled, busybox made...   8                    [OK]
    docker.io   docker.io/odise/busybox-python                                                   3                    [OK]

### is-official

This example displays images with a name containing 'busybox', at least
3 stars and are official builds:

    $ docker search --filter "is-automated=true" --filter "stars=3" busybox
    INDEX       NAME                             DESCRIPTION                                     STARS     OFFICIAL   AUTOMATED
    docker.io   docker.io/progrium/busybox                                                       53                   [OK]
    docker.io   docker.io/radial/busyboxplus     Full-chain, Internet enabled, busybox made...   8                    [OK]
    docker.io   docker.io/odise/busybox-python                                                   3                    [OK]

### Display non-truncated description (--no-trunc)

This example displays images with a name containing 'busybox',
at least 3 stars and the description isn't truncated in the output:

    $ docker search --filter stars=3 --no-trunc busybox
    INDEX       NAME                             DESCRIPTION                                                                               STARS     OFFICIAL   AUTOMATED
    docker.io   docker.io/busybox                Busybox base image.                                                                       436       [OK]
    docker.io   docker.io/progrium/busybox                                                                                                 53                   [OK]
    docker.io   docker.io/radial/busyboxplus     Full-chain, Internet enabled, busybox made from scratch. Comes in git and cURL flavors.   8                    [OK]
    docker.io   docker.io/odise/busybox-python                                                                                             3                    [OK]
