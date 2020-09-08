.PHONY: pb
pb:
	protoc --go_out=. pb/*.proto
