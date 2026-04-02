TAPES := $(wildcard demo/*.tape)
# Exclude test/scratch tapes (prefixed with _)
TAPES := $(filter-out demo/_%.tape,$(TAPES))
GIFS  := $(patsubst demo/%.tape,demo/output/%.gif,$(TAPES))

.PHONY: all clean list

all: $(GIFS)

demo/output/%.gif: demo/%.tape demo/setup.sh | demo/output
	vhs $<

demo/output:
	mkdir -p $@

list:
	@echo "Tapes:"
	@for t in $(TAPES); do echo "  $$t"; done
	@echo "GIFs:"
	@for g in $(GIFS); do echo "  $$g"; done

clean:
	rm -f $(GIFS)
