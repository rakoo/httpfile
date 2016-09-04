# httpfile

httpfile is a simple, minimal HTTP file serving API.

# build and run

Being written in Go, you'll need the official Go toolchain, after which
things are pretty standard:

```shell
$ go build
$ ./httpfile
```

# use

The server runs on the 8080 port.

## Send a file

To send a file, POST it with the following characteristics:

- The method must be POST
- The path must be /
- The name parameter must be set to the name you wish (and must not be
    empty)
- The Content-Length Header must be set to the file's length (if less,
    it will be truncated)
- The Content-Type Header must be set to the file's content type

The response will be a 201 on success, with the Date set to the file's
timestamp and the Location header set to the path to be used for
retrieval. It is not merely the desired filename; it is prepended with a
random string to prevent url guessing, allows file with same name to
coexist (potentially with different content) and allows them to be
separately deleted.

Example with curl:

```shell
$ curl -i -H 'Content-Type: text/plain' -H"Content-Length: 12345" --data-binary @file -XPOST "http://localhost:8080/?name=filename"
HTTP/1.1 100 Continue

HTTP/1.1 201 Created
Location: /?name=0245c59bd232045dbe2716e35588b081c21cbf9381719db7407cd9fa24e6adc2/filename
Date: Sun, 04 Sep 2016 21:08:55 GMT
Content-Length: 0
Content-Type: text/plain; charset=utf-8
```

## Retrieve a file

To retrieve a file, GET it with the following characteristics:

- The method must be GET
- The path must be /
- The name parameter must be set to the *full* name of the file, ie the
random part along with the upload file name. In fact it has to be the
path provided in the POST response under the Location header, untouched
- The If-None-Match may be provided, in which case it should be the full
random string; if the file exists a 304 will be returned
- The If-Modified-Since may be provided, in which case it should be the
modification time; if the file exists a 304 will be returned

The response will have a 200 status code, the content-type guessed from
filename and content, proper Etag (set as the random string in the path)
and Last-Modified and Date headers set to modification time. It will
also contain the full file content, of course.

Example with curl:

```shell
$ curl -i "http://localhost:8080/?name=0245c59bd232045dbe2716e35588b081c21cbf9381719db7407cd9fa24e6adc2/filename"
HTTP/1.1 200 OK
Accept-Ranges: bytes
Content-Length: 3217
Content-Type: text/plain; charset=utf-8
Etag: 0245c59bd232045dbe2716e35588b081c21cbf9381719db7407cd9fa24e6adc2
Last-Modified: Sun, 04 Sep 2016 21:08:55 GMT
Date: Sun, 04 Sep 2016 21:21:37 GMT

<actual content>
```

HEAD requests (to get metadata only) works excatly the same, except of
course the content will not be returned

## Delete a file

To delete a file, DELETE it with the following characteristics:
- The method must be DELETE
- The path must be /
- The name parameter must be set to the *full* name of the file, ie the
random part along with the upload file name. In fact it has to be the
path provided in the POST response under the Location header, untouched

(This is similar to GET/HEAD, with the difference that If-None-Match and
 If-Modified-Since are not checked)

The response will be a 200 status code upon success, and 500 (internal
server error) upon any error (including file not found)

Example with curl:

```shell
> $ curl -i -XDELETE "http://localhost:8080/?name=0245c59bd232045dbe2716e35588b081c21cbf9381719db7407cd9fa24e6adc2/main.go"
HTTP/1.1 200 OK
Date: Sun, 04 Sep 2016 21:25:33 GMT
Content-Length: 0
Content-Type: text/plain; charset=utf-8
```

# Run with vagrant

Vagrant stuff is provided to run this simple server with it. If you have
it configured:

```shell
$ vagrant up
```
