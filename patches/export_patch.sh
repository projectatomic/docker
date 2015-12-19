#!/bin/sh

# usage "./export_patches.sh 14"
# the above will export the latest 14 commits (move them under patches then)
# recommended to do every time we apply a patch on fedora-1.* branch
# so we won't never forget patches

# every time we need to rebase fedora-1.*, branch out from docker master and
# apply all patches under patches/ (might want to cp them to another location)

if [ -z "$1" ]; then
	echo "need to know how many patches from HEAD I need to export!"
	exit
fi

commits=( $(git log --format=format:%H -n "$1" | xargs) )

c="$1"
s="$1"
counter=0
head="HEAD"
while [ "$c" -gt 0 ]
do
	num=$[$s-$c]
	dnum=$[$num+1]
	log="$head~$num"
	commit_msg=$(git log --format=%B -n 1 $log | head -1 | sed -e 's/ /_/g' | sed -e 's/\//_/g')
	commit_msg_extended=$(git log --format=%B -n 1 $log)
	git diff "${commits[$counter]}"^! > "$c-$commit_msg".patch
	echo "$commit_msg_extended" > "$c-$commit_msg".message
	counter=$[$counter+1]
	c=$[$c-1]
done
