# httpmonitor

Simple console program that monitors HTTP traffic.

## Requirements

- Consume an actively written-to w3c-formatted HTTP access log
  (https://en.wikipedia.org/wiki/Common_Log_Format)
- Every 10s, display in the console the sections of the web site with the most
  hits (a section is defined as being what's before the second '/' in a URL.
  i.e. the section for "http://my.site.com/pages/create' is
  "http://my.site.com/pages"), as well as interesting summary statistics on the
  traffic as a whole.
- Make sure a user can keep the console app running and monitor traffic on
  their machine
- Whenever total traffic for the past 2 minutes exceeds a certain number on
  average, add a message saying that “High traffic generated an alert - hits =
  {value}, triggered at {time}”
- Whenever the total traffic drops again below that value on average for the
  past 2 minutes, add another message detailing when the alert recovered
- Make sure all messages showing when alerting thresholds are crossed remain
  visible on the page for historical reasons.
- Write a test for the alerting logic

## Usage

```
$ httpmonitor --file /path/to/http/log
```

For more options, run `httpmonitor --help`.

## Explain how you'd improve on this application design

- For the response size histogram, we use a windowed histogram which we rotate
  periodically to avoid overfilling. We should do the same for the
  probabilistic filters we're using like the count-min sketch (top-k) and
  HyperLogLog.
- We should handle log file rotation. The fsnotify library being used allows us
  to capture when files are created, renamed, etc. in a cross-platform way.
  When a new log file is created, we need to rewind and begin reading from the
  beginning.
- Make the UI prettier.
- Perhaps provide a way to store captured data in a centralized way or send it
  to some such service for storage, querying, etc. (though one is likely just
  storing the logs themselves).
- I'm sure there are more useful traffic statistics to aggregate, but perhaps
  offer a way to let users provide their own custom filters.
- We could expand the windowedAverager to allow averaging other request data
  for the window other than just average hits.
- More unit tests. :)
