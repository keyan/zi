.PHONY: run
run: build
	./zi

.PHONY: build
build:
	go build -o zi

.PHONY: clean
clean:
	@rm -f ./zi ./main *.log .*swo .*swp &> /dev/null
