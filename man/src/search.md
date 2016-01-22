Search Docker Hub for images that match the specified `TERM`. The table
of images returned displays the name, description (truncated by default), number
of stars awarded, whether the image is official, and whether it is automated.

*Note* - Search queries will only return up to 25 results

## Filter

   Filter output based on these conditions:
   - stars=<numberOfStar>
   - is-automated=(true|false)
   - is-official=(true|false)

# EXAMPLES

## Search Docker Hub for ranked images

Search a registry for the term 'fedora' and only display those images
ranked 3 or higher:

    $ docker search --filter=starts=3 fedora
    INDEX      NAME                            DESCRIPTION                                    STARS OFFICIAL  AUTOMATED
    docker.io  docker.io/mattdm/fedora         A basic Fedora image corresponding roughly...  50
    docker.io  docker.io/fedora                (Semi) Official Fedora base image.             38
    docker.io  docker.io/mattdm/fedora-small   A small Fedora image on which to build. Co...  8
    docker.io  docker.io/goldmann/wildfly      A WildFly application server running on a ...  3               [OK]

## Search Docker Hub for automated images

Search Docker Hub for the term 'fedora' and only display automated images
ranked 1 or higher:

    $ docker search --filter=is-automated=true --filter=starts=1 fedora
    INDEX      NAME                         DESCRIPTION                                     STARS OFFICIAL  AUTOMATED
    docker.io  docker.io/goldmann/wildfly   A WildFly application server running on a ...   3               [OK]
    docker.io  docker.io/tutum/fedora-20    Fedora 20 image with SSH access. For the r...   1               [OK]
