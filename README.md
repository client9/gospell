# gospell
[![Build Status](https://travis-ci.org/client9/gospell.svg?branch=master)](https://travis-ci.org/client9/gospell) [![Go Report Card](http://goreportcard.com/badge/client9/gospell)](http://goreportcard.com/report/client9/gospell) [![GoDoc](https://godoc.org/github.com/client9/gospell?status.svg)](https://godoc.org/github.com/client9/gospell) [![Coverage](http://gocover.io/_badge/github.com/client9/gospell)](http://gocover.io/github.com/client9/gospell) [![license](https://img.shields.io/badge/license-MIT-blue.svg?style=flat)](https://raw.githubusercontent.com/client9/gospell/master/LICENSE)

pure golang spelling dictionary based on hunspell dictionaries.

NOTE: I'm not an expert in linguistics nor spelling.  Help is very
welcome!

NOTE: This is not affiliated with Hunspell although if they wanted
merge it in as an official project, I'd be happy to donate code.

http://hunspell.github.io
https://github.com/hunspell


### Where can I get dictionaries?

The world of spelling dictionaries is surprisingly complicated, as
"lists of words" are frequently proprietary and with conflicting
software licenses.  Fortunately, @kevina many open source lists and
maintains the [SCOWL](http://wordlist.aspell.net) project and the
github repo at https://github.com/kevina/wordlist

These lists are then packaged up and reused by various other projects
below.

#### Open Office

http://extensions.openoffice.org/en/project/english-dictionaries-apache-openoffice

The downloaded file has a `.oxt` extenion but it's a compressed `tar`
file.  Extract the files using:

```
mkdir dict-en
cd dict-en
tar -xzf ../dict-en.oxt
```

#### Chromium

The Chrome/Chromium browser uses Hunspell and it's source tree
contains various up-to-date dictionaries, some with additional words.  You can view them at
[chromium.googlesource.com](https://chromium.googlesource.com/chromium/deps/hunspell_dictionaries/+/master)
and you can check them out locally via

```bash
git clone --depth=1 https://chromium.googlesource.com/chromium/deps/hunspell_dictionaries
```

More information can be found in the [chromium developer guide](https://www.chromium.org/developers/how-tos/editing-the-spell-checking-dictionaries)
