build:
	mkdir -p bin
	go build -o ./bin/dai ./cmd

clean:
	rm -rf *.mp4 *.json 

test-small: clean build
	./bin/dai https://rutube.ru/video/2e2efa63e94fe94ba8ef270a7260134c/

test-big: clean build
	./bin/dai https://rutube.ru/video/fc030f6915f11251816e47c3cfdaa59d/ 
