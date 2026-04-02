Output demo/output/01-dry-run.gif
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

# Show the full profile — 35 collectors, 5 analyzers.
# Pipe through less so the viewer can read all of it.
Type "soda --dry-run 2>/dev/null | less"
Enter
Sleep 4s
# Scroll to the second page to reveal analyzers and RBAC.
Type " "
Sleep 4s
# Jump to the end to show the RBAC table.
Type "G"
Sleep 4s
# Exit the pager.
Type "q"
Sleep 1s

# Now show the health profile — a focused subset.
Type "soda --dry-run --profile=health 2>/dev/null"
Enter
Sleep 5s
