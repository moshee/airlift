FROM golang
MAINTAINER moshee <moshee@displaynone.us>
RUN go install ktkr.us/pkg/airlift/cmd/airliftd@latest
EXPOSE 60606
ENTRYPOINT ["airliftd"]
