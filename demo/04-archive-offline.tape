Output demo/output/04-archive-offline.gif
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
# Clean up any previous archive.
Type "rm -f /tmp/soda-demo-archive.tar.gz"
Enter
Sleep 500ms
Show

# Collect and archive in one step.
Type "soda -n production --cluster-name=bubbles-database --profile=health --archive --output-dir=/tmp/soda-demo-archive"
Enter
Sleep 40s

# Show the archive file.
Type "ls -lh soda-demo-archive.tar.gz"
Enter
Sleep 3s

# Re-analyze offline — instant, no cluster connection needed.
Type "soda --from-archive=soda-demo-archive.tar.gz"
Enter
Sleep 5s
