FROM golang
MAINTAINER moshee <moshee@displaynone.us>
RUN go get -u ktkr.us/pkg/airlift/cmd/airliftd
EXPOSE 60606
ENTRYPOINT ["airliftd"]
