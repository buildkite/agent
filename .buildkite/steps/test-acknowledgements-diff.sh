#!/usr/bin/env bash
set -euo pipefail

trap "rm -f ACKNOWLEDGEMENTS-{orig,new}.md" EXIT

# Make a comparison copy without the generate timestamp
sed -e '$d' ACKNOWLEDGEMENTS.md > ACKNOWLEDGEMENTS-orig.md

# Regenerate acknowledgements and trim the date
./scripts/generate-acknowledgements.sh
sed -e '$d' ACKNOWLEDGEMENTS.md > ACKNOWLEDGEMENTS-new.md

if diff -u ACKNOWLEDGEMENTS-{orig,new}.md ; then
  echo "Acknowledgements are up-to-date! 🎉"
  exit 0
fi

echo "The ACKNOWLEDGEMENTS.md file needs to be regenerated."
echo
echo "Fix this by running \`./scripts/generate-acknowledgements.sh\` locally, and committing the result."
exit 1

