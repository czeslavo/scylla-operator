Output demo/output/02-live-run.gif
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
Show

# Run a health-check against the production ScyllaCluster.
Type "soda -n production --cluster-name=bubbles-database --profile=health"
Enter
# Wait for collection to finish (~30s) and the report to render.
Sleep 35s

# Let the viewer read the analysis summary.
Sleep 5s
