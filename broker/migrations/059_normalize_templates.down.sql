-- Revert placeholders in template.subject and template.body
-- Old: {{.BatchQuery}}, {{.ActualCount}}, {{.FullCount}}
-- New: {{batchQuery}}, {{actualCount}}, {{fullCount}}
UPDATE template
SET
    subject = REPLACE(
            REPLACE(
                    REPLACE(subject, '{{.BatchQuery}}', '{{batchQuery}}'),
                    '{{.ActualCount}}', '{{actualCount}}'
            ),
            '{{.FullCount}}', '{{fullCount}}'
              ),
    body    = REPLACE(
            REPLACE(
                    REPLACE(body, '{{.BatchQuery}}', '{{batchQuery}}'),
                    '{{.ActualCount}}', '{{actualCount}}'
            ),
            '{{.FullCount}}', '{{fullCount}}'
              )
WHERE
    subject LIKE '%{{.BatchQuery}}%'
   OR subject LIKE '%{{.ActualCount}}%'
   OR subject LIKE '%{{.FullCount}}%'
   OR body LIKE '%{{.BatchQuery}}%'
   OR body LIKE '%{{.ActualCount}}%'
   OR body LIKE '%{{.FullCount}}%';