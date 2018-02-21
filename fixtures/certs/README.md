## Generating certs with no max path length constraint

### Why do we need it

This cert is used to generate an intermediate ca cert that can be used in the
`instance identity` inigo tests. Unmodified ca certs generated by `certstrap`
have a max path length of 0, which means that no non-self-signed intermediate
certs can be present in the certificate chain.

### How can I regenerate those certs

- modify [this line](https://github.com/square/certstrap/blob/b6aef507a0840bf78bac99e7ffa6e6eb5c2c3c9f/pkix/cert_auth.go#L63) in certstrap to
  ```go
  MaxPathLenZero: false // instead of true
  ```
- run `go install github.com/square/certstrap`
- regenerate the ca certificate