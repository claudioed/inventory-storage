Feature: Reservation
  A Reservation is a REVOCABLE binding of a quantity of Usable inventory to
  demand. Physical delivery can fail, so a Reservation must be releasable and
  re-allocatable; confirming a pick consumes it for good.

  Background:
    Given an empty warehouse
    And a Bin "B-1-1" with capacity 20
    And 10 units of SKU "SKU-R" are Stowed into Bin "B-1-1"

  @bdd
  Scenario: Reserving usable stock succeeds and reduces usable quantity
    When I Reserve 4 units of SKU "SKU-R" for demand "ORDER-1"
    Then the response status is 201
    And the Reservation response reports quantity 4 for demand "ORDER-1"
    And the response has a Location header pointing at the created Reservation
    And the domain event "StockReserved" was published
    And the Usable inventory for SKU "SKU-R" is 6

  @bdd
  Scenario: Reserving more than usable quantity is rejected
    When I Reserve 11 units of SKU "SKU-R" for demand "ORDER-2"
    Then the response status is 409
    And the problem detail type is "insufficient-usable"
    And the Usable inventory for SKU "SKU-R" is 10

  @bdd
  Scenario: Revoking a reservation returns the quantity to usable
    Given a Reservation of 4 units of SKU "SKU-R" for demand "ORDER-3"
    And the Usable inventory for SKU "SKU-R" is 6
    When I Revoke the Reservation
    Then the response status is 204
    And the domain event "ReservationRevoked" was published
    And the Usable inventory for SKU "SKU-R" is 10

  @bdd
  Scenario: Confirming a pick consumes the reservation (StockPicked)
    Given a Reservation of 4 units of SKU "SKU-R" for demand "ORDER-4"
    When I Confirm the pick for the Reservation
    Then the response status is 204
    And the domain event "StockPicked" was published
    And the Usable inventory for SKU "SKU-R" is 6
    When I Revoke the Reservation
    Then the response status is 409
    And the problem detail type is "reservation-already-resolved"
