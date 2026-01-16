all:
	go build -o git-backtrack ./cmd/git-backtrack

run:
	./git-backtrack

clean:
	rm -f git-backtrack