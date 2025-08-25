ACTUAL_PORT="${HTTP_PORT:8081}"
URL="http://localhost:${ACTUAL_PORT}/archive_ill_transactions?archive_delay=240h&archive_status=LoanCompleted,CopyCompleted,Unfilled"

RESPONSE=$(curl -X POST -s -o /dev/null -w "%{http_code}" "${URL}")

if [ "$RESPONSE" -eq 200 ]; then
    echo "Success! HTTP Status Code: ${RESPONSE}"
else
    echo "Error! HTTP Status Code: ${RESPONSE}"
    echo "Check server logs for more details."
fi

echo "Script finished."