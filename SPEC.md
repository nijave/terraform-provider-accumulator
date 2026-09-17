The accumulator provider provides Terraform resources that allow "accumulating" input over time. This is useful if you want a declarative "current" version of something while also being able to maintain a fixed history length without using systems external to Terraform.

This provider defines resources

```hcl
resource "accumulator_list" "historical_list" {
  # A list of 0+ strings
  inputs = ["hello"]
  # The maximum amount of inputs to store
  length = 5
  # Optional[string], should "forget" the inputs history and reset to "inputs" when this value changes
  triggers_reset = null

  # Outputs
  # outputs list[string]: An accumulated list of all the inputs fixed to `length` that works like an LRU such that when new inputs cause this to exceed length, older inputs are forgotten
  # ex inputs = ["a"], length = 1 -> outputs = ["a"] then inputs = ["b"], length = 1 -> outputs = ["b"]
  # ex inputs = ["a"], length = 2 -> outputs = ["a"] then inputs = ["b"], length = 2 -> outputs = ["a", "b"] then inputs = ["c"], length = 2 -> outputs = ["b", "c"]
  # ex inputs = ["a", "b"], length = 1 -> outputs = ["b"]  # the last item(s) always win
  # ex inputs = ["a"], length = 1 -> outputs = ["a"] then inputs = [], length = 1 -> outputs = ["a"] then inputs = [], length = 0 -> outputs = []
}
```

```hcl
resource "accumulator_set" "historical_set" {
  # Remembers every item it's ever seen but only once

  # Required list[string]
  inputs = ["hello"]
  
  # Optional[string], should reset the set to "inputs" when this changes
  triggers_reset = null

  # Outputs
  # outputs set[string]: A set of all inputs ever seen. Ordering is not guaranteed but each item only ever appears once
}
```
