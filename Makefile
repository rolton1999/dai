build:
	mkdir -p bin
	go build -o ./bin/dai ./cmd

clean:
	rm -f ./*.mp4 ./.concat.txt ./*.json ./.tmp

test-single: build
	./bin/dai https://rutube.ru/video/2e2efa63e94fe94ba8ef270a7260134c/
