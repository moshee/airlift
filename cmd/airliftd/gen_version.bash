#!/bin/bash

GIT_TAG=$(git describe --tags HEAD 2>/dev/null)
if [ $? -ne 0 ]; then
   GIT_TAG="git-$(git rev-parse --short HEAD)"
fi
GIT_REV=$(git rev-list HEAD --count)

cat > version.go <<END
package main

import "time"

func init() {
	VERSION = "$GIT_TAG (r$GIT_REV)"
	MODTIME = time.Unix($(date +%s), 0)
}
END
