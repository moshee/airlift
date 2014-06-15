all: server cli clean upload

server:
	cd airlift-server && go build
	tar cfj airlift-server-$(GOOS)_$(GOARCH).tbz airlift-server

cli:
	cd lift && go build
	tar cfj lift-$(GOOS)_$(GOARCH).tbz lift

clean:
	rm -f airlift-server/airlift-server
	rm -f lift/lift

upload:
	lift -n *.tbz
