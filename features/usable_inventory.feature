Feature: Usable inventory
  Usable inventory is on-hand stock minus active Reservations minus
  held/damaged/Unlocated stock. Usable — not total — is what constrains
  release, so it is exposed explicitly.

  Background:
    Given an empty warehouse

  @bdd
  Scenario: Usable inventory reflects on-hand minus active reservations
    Given a Bin "U-1-1" with capacity 30
    And 12 units of SKU "SKU-U" are Stowed into Bin "U-1-1"
    And a Bin "U-1-2" with capacity 30
    And 8 units of SKU "SKU-U" are Stowed into Bin "U-1-2"
    And the Usable inventory for SKU "SKU-U" is 20
    When I Reserve 5 units of SKU "SKU-U" for demand "ORDER-U"
    Then the response status is 201
    When I request the Usable inventory for SKU "SKU-U"
    Then the response status is 200
    And the Usable inventory response reports SKU "SKU-U" with 15 usable
