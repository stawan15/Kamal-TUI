package main

import "errors"

var errBinaryNotFound = errors.New("kamal not found on PATH, in bin/kamal, or via `bundle exec` (Gemfile present but bundle missing)")
