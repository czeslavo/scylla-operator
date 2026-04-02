Output demo/output/03-artifacts.gif
Set Shell "bash"
Set Width 1200
Set Height 800
Set FontSize 16
Set Theme "Dracula"
Set TypingSpeed 40ms

Hide
Type "source ./demo/setup.sh"
Enter
Sleep 1s
# Use a short variable so the commands look clean.
Type "OUT=/tmp/soda-test-run"
Enter
Sleep 500ms
Show

# Show the full directory tree through a pager.
Type "tree $OUT --dirsfirst | less"
Enter
Sleep 3s
# Scroll through pages to show the depth.
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
# Jump to the end to show the summary line.
Type "G"
Sleep 2s
Type "q"
Sleep 1s

# Peek at a real artifact — OS info from a Scylla node.
Type "cat $OUT/collectors/per-scylla-node/production/bubbles-database-us-east-1-us-east-1a-0/OSInfoCollector/os-release.log"
Enter
Sleep 4s

# Show the self-describing README through a pager.
Type "less $OUT/README.md"
Enter
Sleep 3s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type " "
Sleep 2s
Type "G"
Sleep 2s
Type "q"
Sleep 1s
