<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
                xmlns:svrl="http://purl.oclc.org/dsdl/svrl"
                version="2.0">

  <!-- Schematron "vide" : ne fait aucun contrôle, valide toujours le XML en entrée. -->

  <xsl:output method="xml" omit-xml-declaration="no" indent="yes"/>

  <xsl:template match="/">
    <svrl:schematron-output title="EREPORTING (vide — aucun contrôle)" schemaVersion="ISO19757-3"/>
  </xsl:template>

</xsl:stylesheet>
