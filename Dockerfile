FROM golang
MAINTAINER moshee <moshee@displaynone.us>
RUN go get -d -u ktkr.us/pkg/airlift
WORKDIR /go/src/ktkr.us/pkg/airlift
RUN go build
EXPOSE 60606
ENTRYPOINT ["./airlift"]
